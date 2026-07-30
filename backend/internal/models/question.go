package models

import "github.com/google/uuid"

type Question struct{ 
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Content string `gorm:"not null"`

	ExamID uuid.UUID `gorm:"type:uuid;not null;index"`
}