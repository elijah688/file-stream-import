package model

import "github.com/google/uuid"

type Location struct {
	ID          uuid.UUID `json:"id"`
	LocID       string    `json:"locid"`
	LocTimeZone string    `json:"loctimezone"`
	Country     string    `json:"country"`
	LocName     string    `json:"locname"`
	Business    string    `json:"business"`
}
