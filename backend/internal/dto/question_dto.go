package dto

import "github.com/google/uuid"

type QuestionRespone struct{
	ID uuid.UUID `json:"id"`
	Content string `json:"content"`
	IsCorrect bool `json:"isCorrect"`
}
