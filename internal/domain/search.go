package domain

// SearchItem represents an indexed ONU for global search
type SearchItem struct {
	OltID        string `json:"oltId"`
	Board        int    `json:"board"`
	Pon          int    `json:"pon"`
	OnuID        int    `json:"onuId"`
	Name         string `json:"name"`
	SerialNumber string `json:"serialNumber"`
	Status       string `json:"status"` // "Online", "Offline", "LOS"
}
