package dto

import "github.com/google/uuid"

type ChoiceResponse struct{
	ID uuid.UUID `json:"id"`
	Content string `json:"content"`
}

