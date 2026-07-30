package models

import "github.com/google/uuid"


type User struct{
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Email string `gorm:"size:100;uniqueIndex;not null"`
}