package dto

import (
	"github.com/google/uuid"
)

type ExamsWithOutFk struct{
	ID uuid.UUID `json:"id"`
	Title string `json:"title"`
}


type CourseWithLessons struct{
	ID uuid.UUID  `json:"id"`
	Title string    `json:"title"`
	IsPublic bool  `json:"isPublic"`
	
	UserID uuid.UUID `json:"userId"`
	LessonsWithExams []LessonWithExam  `json:"lessons"`
}

type CourseResponse struct{
	ID uuid.UUID `json:"id"`
	Title string 	`json:"title"`
	IsPublic bool 	`json:"isPublic"`
}