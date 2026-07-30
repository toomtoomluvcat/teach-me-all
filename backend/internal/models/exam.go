package models

import "github.com/google/uuid"

type Exam struct{
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Title string `gorm:"size:100;not null"`

	LessonID uuid.UUID `gorm:"type:uuid;not null;index"`
}