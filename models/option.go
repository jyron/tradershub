package models

type OptionContract struct {
	Symbol         string  `json:"symbol"`
	UnderlyingSymbol string `json:"underlying_symbol"`
	OptionType     string  `json:"type"` // "call" or "put"
	StrikePrice    float64 `json:"strike_price"`
	ExpirationDate string  `json:"expiration_date"`
	Bid            float64 `json:"bid"`
	Ask            float64 `json:"ask"`
	LastPrice      float64 `json:"last_price"`
	Volume         int64   `json:"volume"`
	OpenInterest   int64   `json:"open_interest"`
}

type OptionChainResponse struct {
	UnderlyingSymbol string           `json:"underlying_symbol"`
	Contracts        []OptionContract `json:"contracts"`
}

type OptionTradeRequest struct {
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"` // "buy" or "sell"
	Quantity       int     `json:"quantity"`
	Reasoning      string  `json:"reasoning"`
}

type OptionTradeResponse struct {
	TradeID        string  `json:"trade_id"`
	Status         string  `json:"status"`
	Symbol         string  `json:"symbol"`
	UnderlyingSymbol string `json:"underlying_symbol"`
	OptionType     string  `json:"type"`
	StrikePrice    float64 `json:"strike_price"`
	ExpirationDate string  `json:"expiration_date"`
	Side           string  `json:"side"`
	Quantity       int     `json:"quantity"`
	Price          float64 `json:"price"`
	Total          float64 `json:"total"`
	ExecutedAt     string  `json:"executed_at"`
}
