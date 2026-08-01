package models

import "github.com/google/uuid"

type Choice struct{ 
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Content string `gorm:"size:100;not null"`
	IsCorrect bool `gorm:"not null;default:false"`
	QuestionID uuid.UUID `gorm:"type:uuid;not null;index"`
}