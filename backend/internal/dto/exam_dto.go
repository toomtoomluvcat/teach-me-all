package dto

import "github.com/google/uuid"

type ExamResponse struct{
	ID uuid.UUID `json:"id"` 
	Title string `json:"title"`
}

type ExamWithQuestions struct{
	ID uuid.UUID `json:"id"`	
	Title string `json:"content"`

	Questions []QuestionWithChoice `json:"questions"`
}
