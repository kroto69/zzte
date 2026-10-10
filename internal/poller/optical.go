package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"olt-monitor/internal/cache"
	"olt-monitor/internal/config"
	"olt-monitor/internal/domain"
	"olt-monitor/internal/service"
	"olt-monitor/internal/snmp"
)

type OpticalPoller struct {
	manager *service.OLTManager
	cache   *cache.RedisCache
	cfg     *config.Config
	cancel  context.CancelFunc
	done    chan struct{}
	hot     *service.HotTracker // PATCH(oltwtch-2): prioritas port panas (opsional)

	// PATCH(oltwtch): cache daftar PON (guard anti-tumpuk ada di closure per-OLT
	// di Start, karena satu siklus 32 PON bisa lebih lama dari interval ticker).
	ponsMu   sync.RWMutex
	pons     map[string][]ponPort // oltID -> daftar PON (di-discover sekali saja)
	ponsTime map[string]time.Time

	cyclesMu sync.Mutex
	cycles   map[string]int // oltID -> jumlah siklus (untuk tier dingin)
}

// NewOpticalPoller — parameter hotTracker opsional (variadic) agar kode lama kompil.
func NewOpticalPoller(manager *service.OLTManager, cache *cache.RedisCache, cfg *config.Config, hot ...*service.HotTracker) *OpticalPoller {
	p := &OpticalPoller{
		manager:  manager,
		cache:    cache,
		cfg:      cfg,
		done:     make(chan struct{}),
		pons:     make(map[string][]ponPort),
		ponsTime: make(map[string]time.Time),
		cycles:   make(map[string]int),
	}
	if len(hot) > 0 {
		p.hot = hot[0]
	}
	return p
}

// hotWindow: port dianggap panas bila dibuka user dalam 15 menit terakhir.
const hotWindow = 15 * time.Minute

// coldEvery: port dingin dipoll tiap N siklus (hemat beban OLT).
const coldEvery = 6

func (p *OpticalPoller) Start(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	p.cancel = cancel

	go func() {
		defer close(p.done)

		olts := p.manager.ListOLTs()
		if len(olts) == 0 {
			log.Warn().Msg("Tidak ada OLT terdaftar, optical poller idle")
			<-ctx.Done()
			return
		}

		globalInterval := time.Duration(p.cfg.OpticalPoller.Interval) * time.Second
		if globalInterval <= 0 {
			globalInterval = 60 * time.Second
		}

		var wg sync.WaitGroup

		for i := range olts {
			olt := olts[i]
			interval := time.Duration(olt.Config.PollInterval) * time.Second
			if interval <= 0 {
				interval = globalInterval
			}

			wg.Add(1)
			go func(oltID string, ivl time.Duration) {
				defer wg.Done()
				log.Info().Str("oltId", oltID).Dur("interval", ivl).Msg("Optical poller dimulai untuk OLT")

				// PATCH(oltwtch): guard per-OLT — jangan mulai siklus baru kalau
				// yang lama belum selesai. Ini yang bikin OLT "dikeroyok" poller.
				var polling atomic.Bool

				// PATCH(oltwtch-2): circuit breaker — kalau OLT error/berat
				// berturut-turut, mundurkan jadwal (exponential backoff, maks 1 jam)
				// agar OLT sempat pulih, bukan makin ditimpa.
				var consecutiveErrors int
				var skipUntil time.Time

				if err := p.pollOLT(ctx, oltID); err != nil && ctx.Err() == nil {
					log.Error().Err(err).Str("oltId", oltID).Msg("Poll pertama gagal")
				}

				ticker := time.NewTicker(ivl)
				defer ticker.Stop()

				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if time.Now().Before(skipUntil) {
							log.Warn().Str("oltId", oltID).
								Msg("Circuit breaker: OLT sedang bermasalah, siklus dilewati")
							continue
						}
						if !polling.CompareAndSwap(false, true) {
							log.Warn().Str("oltId", oltID).
								Msg("Siklus poll sebelumnya belum selesai, lewati")
							continue
						}
						cycleStart := time.Now()
						err := p.pollOLT(ctx, oltID)
						polling.Store(false)
						if err != nil && ctx.Err() == nil {
							log.Error().Err(err).Str("oltId", oltID).Msg("Poll gagal")
							consecutiveErrors++
							backoff := ivl
							for i := 0; i < consecutiveErrors && backoff < time.Hour; i++ {
								backoff *= 2
							}
							skipUntil = time.Now().Add(backoff)
							log.Warn().Str("oltId", oltID).Dur("backoff", backoff).
								Int("gagal_berturut", consecutiveErrors).
								Msg("Polling dijeda sementara")
						} else {
							consecutiveErrors = 0
							if d := time.Since(cycleStart); d > ivl {
								log.Warn().Str("oltId", oltID).Dur("durasi", d).Dur("interval", ivl).
									Msg("Siklus lebih lama dari interval — pertimbangkan interval lebih besar")
							}
						}
					}
				}
			}(olt.ID, interval)
		}

		<-ctx.Done()
		wg.Wait()
		log.Info().Msg("Optical poller dihentikan")
	}()
}

func (p *OpticalPoller) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	<-p.done
	log.Info().Msg("Optical poller berhenti")
}

func (p *OpticalPoller) IsEnabled() bool {
	return p.cfg.OpticalPoller.Enabled
}

type ponPort struct {
	board       int
	pon         int
	ifIndex     int
	description string
}

func (p *OpticalPoller) discoverPONs(client *snmp.Client) ([]ponPort, error) {
	results, err := client.BulkWalk(snmp.OIDPONDescription)
	if err != nil {
		return nil, fmt.Errorf("gagal walk PON descriptions: %w", err)
	}

	var pons []ponPort
	for _, pdu := range results {
		suffix := strings.TrimPrefix(pdu.Name, snmp.OIDPONDescription+".")
		if suffix == pdu.Name {
			continue
		}
		ifIndex, err := strconv.Atoi(suffix)
		if err != nil {
			continue
		}
		_, slot, port := snmp.ParseIfIndex(ifIndex)
		desc := strings.Trim(snmp.PduToString(&pdu), "\"")
		pons = append(pons, ponPort{board: slot, pon: port, ifIndex: ifIndex, description: desc})
	}

	return pons, nil
}

func (p *OpticalPoller) pollOLT(ctx context.Context, oltID string) error {
	client, err := p.manager.GetNewClient(oltID)
	if err != nil {
		return fmt.Errorf("gagal mendapat client SNMP untuk OLT %s: %w", oltID, err)
	}

	if err := client.Connect(); err != nil {
		return fmt.Errorf("gagal konek ke OLT %s: %w", oltID, err)
	}
	defer client.Close()

	log.Debug().Str("oltId", oltID).Msg("Mulai polling OLT")

	// PATCH(oltwtch): daftar PON hampir tidak pernah berubah — discover sekali
	// saja per 24 jam, jangan walk ulang tiap siklus.
	pons, err := p.getPONs(ctx, oltID, client)
	if err != nil {
		return fmt.Errorf("gagal discover PON untuk OLT %s: %w", oltID, err)
	}

	if len(pons) == 0 {
		log.Warn().Str("oltId", oltID).Msg("Tidak ada PON ditemukan")
		return nil
	}

	// PATCH(oltwtch-2): tiered polling. Port panas (baru dibuka user) dipoll
	// tiap siklus; port dingin cukup tiap coldEvery siklus. Tanpa tracker,
	// semua dianggap panas (perilaku lama).
	// PATCH(oltwtch-3): STAGGER. Port dingin tidak lagi diborong tiap
	// coldEvery siklus (ledakan beban 6x -> memicu degraded mode di port
	// padat, terbukti via simulasi). Tiap port dingin dapat slot rotasi
	// deterministik (board/pon % coldEvery) sehingga beban rata tiap siklus.
	// Tiap port dingin tetap dikunjungi tiap coldEvery siklus.
	p.cyclesMu.Lock()
	p.cycles[oltID]++
	cycle := p.cycles[oltID]
	p.cyclesMu.Unlock()

	active := make([]ponPort, 0, len(pons))
	for _, pp := range pons {
		if p.hot.IsHot(oltID, pp.board, pp.pon, hotWindow) {
			active = append(active, pp)
			continue
		}
		slot := (pp.board*32 + pp.pon) % coldEvery
		if cycle%coldEvery == slot {
			active = append(active, pp)
		}
	}
	if len(active) == 0 {
		log.Debug().Str("oltId", oltID).Msg("Tidak ada PON panas/dingin jatuh tempo, lewati")
		return nil
	}
	pons = active

	// Simpan daftar PON ke cache per board (TTL 24 jam, jarang berubah)
	if p.cache != nil {
		boardPons := make(map[int][]domain.PONInfo)
		for _, pp := range pons {
			boardPons[pp.board] = append(boardPons[pp.board], domain.PONInfo{
				Board:       pp.board,
				Pon:         pp.pon,
				Description: pp.description,
			})
		}
		for board, ponList := range boardPons {
			sort.Slice(ponList, func(i, j int) bool { return ponList[i].Pon < ponList[j].Pon })
			if data, err := json.Marshal(ponList); err == nil {
				if err := p.cache.SetPONListWithTTL(ctx, oltID, board, data, 24*time.Hour); err != nil {
					log.Warn().Err(err).Str("oltId", oltID).Int("board", board).Msg("Gagal simpan PON list ke cache")
				}
			}
		}
	}

	olt, _ := p.manager.GetOLT(oltID)
	pollInterval := time.Duration(p.cfg.OpticalPoller.Interval) * time.Second
	if olt != nil && olt.Config.PollInterval > 0 {
		pollInterval = time.Duration(olt.Config.PollInterval) * time.Second
	}
	if pollInterval <= 0 {
		pollInterval = 60 * time.Second
	}
	ttl := pollInterval * 2

	adapter, _ := p.manager.GetAdapter(oltID)

	sem := make(chan struct{}, 2)

	type ponResult struct {
		count int
		err   error
	}
	results := make([]ponResult, len(pons))
	var wg sync.WaitGroup

	for i, pp := range pons {
		wg.Add(1)
		go func(idx int, pt ponPort) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			count, err := p.pollPON(ctx, client, adapter, oltID, pt.board, pt.pon, pt.ifIndex, ttl)
			results[idx] = ponResult{count: count, err: err}
			time.Sleep(200 * time.Millisecond)
		}(i, pp)
	}
	wg.Wait()

	totalOnus := 0
	failedPons := 0
	for i, r := range results {
		if r.err != nil {
			log.Warn().Err(r.err).Str("oltId", oltID).Int("board", pons[i].board).Int("pon", pons[i].pon).Msg("Gagal polling PON")
			failedPons++
			continue
		}
		totalOnus += r.count
	}

	log.Info().Str("oltId", oltID).Int("pons", len(pons)).Int("onus", totalOnus).Int("failed", failedPons).Msg("Polling OLT selesai")
	return nil
}

// getPONs mengembalikan daftar PON; discover via SNMP hanya bila cache kosong
// atau lebih tua dari 24 jam.
func (p *OpticalPoller) getPONs(ctx context.Context, oltID string, client *snmp.Client) ([]ponPort, error) {
	p.ponsMu.RLock()
	pons, ok := p.pons[oltID]
	ts := p.ponsTime[oltID]
	p.ponsMu.RUnlock()
	if ok && len(pons) > 0 && time.Since(ts) < 24*time.Hour {
		return pons, nil
	}

	pons, err := p.discoverPONs(client)
	if err != nil {
		return nil, err
	}
	p.ponsMu.Lock()
	p.pons[oltID] = pons
	p.ponsTime[oltID] = time.Now()
	p.ponsMu.Unlock()
	return pons, nil
}

func (p *OpticalPoller) pollPON(ctx context.Context, client *snmp.Client, adapter snmp.FirmwareAdapter, oltID string, board, pon, ifIndex int, ttl time.Duration) (int, error) {
	// PATCH(oltwtch): poller menghormati lock per-PON yang sama dengan request
	// on-demand — tidak ada lagi walk ganda ke PON yang sama dari dua jalur.
	release, err := p.manager.AcquirePONLock(ctx, oltID, board, pon)
	if err != nil {
		return 0, fmt.Errorf("gagal lock PON %d/%d: %w", board, pon, err)
	}
	defer release()

	statusOID := snmp.BuildWalkOID(snmp.OIDONUStatus, ifIndex)
	statusResults, err := client.BulkWalk(statusOID)
	if err != nil {
		return 0, fmt.Errorf("walk status gagal untuk board %d pon %d: %w", board, pon, err)
	}

	type tempONU struct {
		Status     string
		StatusCode int
		RXPower    float64
	}

	onuMap := make(map[int]*tempONU)

	for _, pdu := range statusResults {
		suffix := strings.TrimPrefix(pdu.Name, statusOID+".")
		parts := strings.Split(suffix, ".")
		if len(parts) >= 1 && parts[0] != "" {
			onuID, err := strconv.Atoi(parts[0])
			if err != nil {
				continue
			}
			onuMap[onuID] = &tempONU{
				Status:     snmp.ParseONUStatus(&pdu),
				StatusCode: snmp.ParseStatusToInt(&pdu),
			}
		}
	}

	if len(onuMap) == 0 {
		return 0, nil
	}

	rxOID := snmp.BuildWalkOID(snmp.OIDONURXPower, ifIndex)
	rxResults, err := client.BulkWalk(rxOID)
	if err == nil {
		for _, pdu := range rxResults {
			suffix := strings.TrimPrefix(pdu.Name, rxOID+".")
			parts := strings.Split(suffix, ".")
			if len(parts) >= 2 && parts[0] != "" {
				onuID, err := strconv.Atoi(parts[0])
				if err != nil {
					continue
				}
				if onu, ok := onuMap[onuID]; ok {
					raw := snmp.GetPDUInt(&pdu)
					if adapter != nil {
						onu.RXPower = adapter.ConvertPower(raw)
					} else {
						onu.RXPower = domain.ConvertPower(raw)
					}
				}
			}
		}
	}

	items := make([]domain.ONUListItem, 0, len(onuMap))
	for onuID, o := range onuMap {
		items = append(items, domain.ONUListItem{
			OltID:      oltID,
			Board:      board,
			Pon:        pon,
			OnuID:      onuID,
			Status:     o.Status,
			StatusCode: o.StatusCode,
			RXPower:    o.RXPower,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].OnuID < items[j].OnuID
	})

	// Merge name + serialNumber dari names cache (TTL 10 jam)
	if p.cache != nil {
		namesData, err := p.cache.GetONUNames(ctx, oltID, board, pon)
		if err == nil && len(namesData) > 0 {
			var names []domain.ONUNameEntry
			if json.Unmarshal(namesData, &names) == nil {
				namesMap := make(map[int]domain.ONUNameEntry, len(names))
				for _, n := range names {
					namesMap[n.OnuID] = n
				}
				for i := range items {
					if n, ok := namesMap[items[i].OnuID]; ok {
						items[i].Name = n.Name
						items[i].SerialNumber = n.SerialNumber
					}
				}
			}
		}
	}

	data, err := json.Marshal(items)
	if err != nil {
		log.Warn().Err(err).Str("oltId", oltID).Int("board", board).Int("pon", pon).Msg("Gagal marshal data ONU")
		return len(items), nil
	}

	if p.cache != nil {
		if err := p.cache.SetONUListWithTTL(ctx, oltID, board, pon, data, ttl); err != nil {
			log.Warn().Err(err).Str("oltId", oltID).Int("board", board).Int("pon", pon).Msg("Gagal simpan ke cache")
		}
	}

	return len(items), nil
}

func (p *OpticalPoller) TriggerPoll() {
	go func() {
		if err := p.triggerPollAll(context.Background()); err != nil {
			log.Error().Err(err).Msg("Triggered poll gagal")
		}
	}()
}

func (p *OpticalPoller) triggerPollAll(ctx context.Context) error {
	olts := p.manager.ListOLTs()
	var g multiErrGroup
	for i := range olts {
		olt := olts[i]
		g.Go(func() error {
			return p.pollOLT(ctx, olt.ID)
		})
	}
	return g.Wait()
}

type multiErrGroup struct {
	errs []error
	mu   sync.Mutex
	wg   sync.WaitGroup
}

func (g *multiErrGroup) Go(fn func() error) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if err := fn(); err != nil {
			g.mu.Lock()
			g.errs = append(g.errs, err)
			g.mu.Unlock()
		}
	}()
}

func (g *multiErrGroup) Wait() error {
	g.wg.Wait()
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.errs) > 0 {
		return g.errs[0]
	}
	return nil
}