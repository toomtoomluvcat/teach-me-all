package main

import (
	"log"
	"teach_me_all/internal/config"
	"teach_me_all/internal/database"

	"github.com/gofiber/fiber/v3"
)

func main(){
	cfg,err := config.Load()
	if err!=nil{
		log.Fatal(err)
	}

	if err:= database.RunMigrate(cfg.DatabaseUrl);err!=nil{
		log.Fatal(err)
	}

	sqlDB,err:=database.Connect(cfg.DatabaseUrl);
	
	if err!=nil{
		log.Fatal(err)
	}

	_ = sqlDB


	app := fiber.New()

	app.Get("/",func(c fiber.Ctx)error {
			return  c.SendString("Hello world")
	})
	app.Listen(":8080")	
}