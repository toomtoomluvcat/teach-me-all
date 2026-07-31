package dto

import (
	"github.com/google/uuid"
)

type LessonWithExam struct{
	ID uuid.UUID `json:"id"`
	Title string `json:"title"`

	Exams []ExamResponse `json:"exams"`
}