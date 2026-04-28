package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"olt-monitor/internal/cache"
	"olt-monitor/internal/config"
	"olt-monitor/internal/domain"
	"olt-monitor/internal/service"
	"olt-monitor/internal/snmp"
)

type NamesPoller struct {
	manager *service.OLTManager
	cache   *cache.RedisCache
	cfg     *config.Config
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewNamesPoller(manager *service.OLTManager, cache *cache.RedisCache, cfg *config.Config) *NamesPoller {
	return &NamesPoller{
		manager: manager,
		cache:   cache,
		cfg:     cfg,
		done:    make(chan struct{}),
	}
}

func (p *NamesPoller) Start(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	p.cancel = cancel

	go func() {
		defer close(p.done)

		olts := p.manager.ListOLTs()
		if len(olts) == 0 {
			log.Warn().Msg("Tidak ada OLT terdaftar, names poller idle")
			<-ctx.Done()
			return
		}

		globalInterval := time.Duration(p.cfg.NamesPoller.Interval) * time.Second
		if globalInterval <= 0 {
			globalInterval = 8 * time.Hour
		}

		var wg sync.WaitGroup

		for i := range olts {
			olt := olts[i]
			interval := globalInterval

			wg.Add(1)
			go func(oltID string, ivl time.Duration) {
				defer wg.Done()
			log.Info().Str("oltId", oltID).Dur("interval", ivl).Msg("Names poller dimulai untuk OLT")

			time.Sleep(30 * time.Second)

			if _, err := p.pollOLT(ctx, oltID); err != nil && ctx.Err() == nil {
					log.Error().Err(err).Str("oltId", oltID).Msg("Names poll pertama gagal")
				}

				ticker := time.NewTicker(ivl)
				defer ticker.Stop()

				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if _, err := p.pollOLT(ctx, oltID); err != nil && ctx.Err() == nil {
						log.Error().Err(err).Str("oltId", oltID).Msg("Names poll gagal")
					}
					}
				}
			}(olt.ID, interval)
		}

		<-ctx.Done()
		wg.Wait()
		log.Info().Msg("Names poller dihentikan")
	}()
}

func (p *NamesPoller) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	<-p.done
	log.Info().Msg("Names poller berhenti")
}

func (p *NamesPoller) pollOLT(ctx context.Context, oltID string) (int, error) {
	client, err := p.manager.GetNewClient(oltID)
	if err != nil {
		return 0, fmt.Errorf("gagal mendapat client SNMP: %w", err)
	}

	if err := client.Connect(); err != nil {
		return 0, fmt.Errorf("gagal konek ke OLT %s: %w", oltID, err)
	}
	defer client.Close()

	pons, err := p.discoverPONs(client)
	if err != nil {
		return 0, fmt.Errorf("gagal discover PON: %w", err)
	}

	if len(pons) == 0 {
		log.Warn().Str("oltId", oltID).Msg("Tidak ada PON ditemukan untuk names poll")
		return 0, nil
	}

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
			count, err := p.pollPON(ctx, client, oltID, pt.board, pt.pon, pt.ifIndex)
			results[idx] = ponResult{count: count, err: err}
			time.Sleep(200 * time.Millisecond)
		}(i, pp)
	}
	wg.Wait()

	total := 0
	for i, r := range results {
		if r.err != nil {
			log.Warn().Err(r.err).Str("oltId", oltID).Int("board", pons[i].board).Int("pon", pons[i].pon).Msg("Names poll PON gagal")
			continue
		}
		total += r.count
	}

	log.Info().Str("oltId", oltID).Int("totalNames", total).Msg("Names poll OLT selesai")
	return total, nil
}

func (p *NamesPoller) discoverPONs(client *snmp.Client) ([]ponPort, error) {
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
		pons = append(pons, ponPort{board: slot, pon: port, ifIndex: ifIndex})
	}

	return pons, nil
}

func (p *NamesPoller) pollPON(ctx context.Context, client *snmp.Client, oltID string, board, pon, ifIndex int) (int, error) {
	nameOID := snmp.BuildWalkOID(snmp.OIDONUName, ifIndex)
	nameResults, err := client.BulkWalk(nameOID)
	if err != nil {
		return 0, fmt.Errorf("walk name gagal untuk board %d pon %d: %w", board, pon, err)
	}

	type tempEntry struct {
		Name         string
		SerialNumber string
	}

	onuMap := make(map[int]*tempEntry)

	for _, pdu := range nameResults {
		suffix := strings.TrimPrefix(pdu.Name, nameOID+".")
		parts := strings.Split(suffix, ".")
		if len(parts) >= 1 && parts[0] != "" {
			onuID, err := strconv.Atoi(parts[0])
			if err != nil {
				continue
			}
			onuMap[onuID] = &tempEntry{
				Name: strings.TrimSpace(snmp.PduToString(&pdu)),
			}
		}
	}

	if len(onuMap) == 0 {
		return 0, nil
	}

	snOID := snmp.BuildWalkOID(snmp.OIDONUSerialNumber, ifIndex)
	snResults, err := client.BulkWalk(snOID)
	if err == nil {
		for _, pdu := range snResults {
			suffix := strings.TrimPrefix(pdu.Name, snOID+".")
			parts := strings.Split(suffix, ".")
			if len(parts) >= 1 && parts[0] != "" {
				onuID, err := strconv.Atoi(parts[0])
				if err != nil {
					continue
				}
				if entry, ok := onuMap[onuID]; ok {
					entry.SerialNumber = snmp.ParseSerialNumber(&pdu)
				}
			}
		}
	}

	names := make([]domain.ONUNameEntry, 0, len(onuMap))
	for onuID, entry := range onuMap {
		names = append(names, domain.ONUNameEntry{
			OnuID:        onuID,
			Name:         entry.Name,
			SerialNumber: entry.SerialNumber,
		})
	}

	sort.Slice(names, func(i, j int) bool {
		return names[i].OnuID < names[j].OnuID
	})

	if p.cache != nil {
		data, err := json.Marshal(names)
		if err != nil {
			log.Warn().Err(err).Str("oltId", oltID).Int("board", board).Int("pon", pon).Msg("Gagal marshal names")
			return len(names), nil
		}
		if err := p.cache.SetONUNames(ctx, oltID, board, pon, data); err != nil {
			log.Warn().Err(err).Str("oltId", oltID).Int("board", board).Int("pon", pon).Msg("Gagal simpan names ke cache")
		}
	}

	log.Debug().Str("oltId", oltID).Int("board", board).Int("pon", pon).Int("count", len(names)).Msg("Names poll PON selesai")
	return len(names), nil
}

func (p *NamesPoller) TriggerPoll() {
	go func() {
		for _, olt := range p.manager.ListOLTs() {
			if _, err := p.pollOLT(context.Background(), olt.ID); err != nil {
				log.Error().Err(err).Str("oltId", olt.ID).Msg("Triggered names poll gagal")
			}
		}
	}()
}