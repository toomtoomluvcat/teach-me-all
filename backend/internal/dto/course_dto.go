package dto

import (
	"teach_me_all/internal/models"

	"github.com/google/uuid"
)

type ExamsWithOutFk struct{
	ID uuid.UUID `json:"id"`
	Title string `json:"title"`
}

type LessonWithExams struct{
	ID uuid.UUID `json:"id"`
	Title string  `json:"title"`
	
	Exams []models.Exam `json:"exams"`
}

type CourseWithLessons struct{
	ID uuid.UUID  `json:"id"`
	Title string    `json:"title"`
	IsPublic bool  `json:"isPublic"`
	
	UserID uuid.UUID `json:"userId"`
	LessonsWithExams []LessonWithExams  `json:"lessons"`
}