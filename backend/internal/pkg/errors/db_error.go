package apperror

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)


func MapDBError(err error) error{
	if err ==nil{
		return  nil
	}
	
	if errors.Is(err,gorm.ErrRecordNotFound){
		return fiber.NewError(fiber.StatusNotFound,"record not found")
	}

	return fiber.NewError(fiber.StatusInternalServerError,"internal server error")
}