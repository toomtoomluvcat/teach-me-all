package dto

import "github.com/google/uuid"

type ExamResponse struct{
	ID uuid.UUID `json:"id"` 
	Title string `json:"title"`
	HasTaken bool `json:"hasTaken"`
}

type ExamWithQuestions struct{
	ID uuid.UUID `json:"id"`	
	Title string `json:"content"`
	HasTaken bool `json:"hasTaken"`
	Questions []QuestionWithChoice `json:"questions"`
}

type ExamAnswers struct{
	ID uuid.UUID `json:"id"`	

	QuestionAnswers []QuestionAnswer `json:"questionAnswers"`
}

