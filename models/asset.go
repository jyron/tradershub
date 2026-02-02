package models

import "time"

type Asset struct {
	Symbol    string    `json:"symbol"`
	Name      string    `json:"name"`
	Exchange  string    `json:"exchange"`
	Tradable  bool      `json:"tradable"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AssetsResponse struct {
	Assets []Asset `json:"assets"`
	Count  int     `json:"count"`
}
