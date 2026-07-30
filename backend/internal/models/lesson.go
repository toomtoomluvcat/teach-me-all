package models

import "github.com/google/uuid"

type Lesson struct{ 
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Title string `gorm:"size:100;not null"`
	
	CourseID uuid.UUID `gorm:"type:uuid;not null;index"`
}