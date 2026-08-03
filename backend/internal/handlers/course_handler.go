package handler

import (
	"fmt"
	"teach_me_all/internal/repository"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type CourseHandler struct{
	repo repository.CourseRepository
}

func NewCourseHandler(repo repository.CourseRepository) *CourseHandler{
	return &CourseHandler{repo:repo}
}

func (h *CourseHandler) CreateCourseByPDF(c fiber.Ctx) error{
	form,err:= c.MultipartForm()
	if err !=nil{
		return  c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":"invalid mutipart file .PDF",
		})
	}
	files:= form.File["files"] //must match field form frontend use
	if len(files) == 0 {
		return  c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":"at least one .PDF file is required",
		})
	}
	
	
	for _,fileHeader := range files{
		 if fileHeader.Header.Get("Content-Type") != "application/pdf"{
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":fmt.Sprintf("%s is not a valid PDF",fileHeader.Filename),
			})
		 }
		 fmt.Println("[Upload] recived PDF:",fileHeader.Filename)
	}
	return nil
}

func (h *CourseHandler) GetCoursesByUserID(c fiber.Ctx) error{
	id := c.Params("id")
	if _,err:=uuid.Parse(id);err!=nil{
		return  c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":"invalid uuid format",
		})
	}
	result,err:=h.repo.GetCoursesByUserID(c.Context(),id)
	if err!=nil{
		return  err
	}
	return  c.JSON(result)
}

func (h *CourseHandler) GetCourseByID(c fiber.Ctx) error{
	id := c.Params("id")
	
	if _,err := uuid.Parse(id);err!=nil{
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":"invalid uuid format",
		})
		}

	result,err:=h.repo.GetCourseByID(c.Context(),id)
	if err!=nil{
		return  err
	}

	return c.JSON(fiber.Map{
		"success":true,
		"data":result,
	})
	
}


