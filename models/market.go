package models

import "time"

type Quote struct {
	Symbol        string    `json:"symbol"`
	Price         float64   `json:"price"`
	Bid           float64   `json:"bid"`
	Ask           float64   `json:"ask"`
	Volume        int64     `json:"volume"`
	Change        float64   `json:"change"`
	ChangePercent float64   `json:"change_percent"`
	Timestamp     time.Time `json:"timestamp"`
}

type QuotesResponse struct {
	Quotes []Quote `json:"quotes"`
}
