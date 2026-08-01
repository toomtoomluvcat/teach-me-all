package dto

import "github.com/google/uuid"

type QuestionRespone struct{
	ID uuid.UUID `json:"id"`
	Content string `json:"content"`
}


type QuestionWithChoice struct{
	ID uuid.UUID `json:"id"`
	Content string `json:"content"`
	Choices []ChoiceResponse `json:"choices"`
}