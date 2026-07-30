package main

import (
	"log"
	"teach_me_all/internal/config"
	"teach_me_all/internal/database"
	handler "teach_me_all/internal/handlers"
	"teach_me_all/internal/routes"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-migrate/migrate/v4"
)

func main(){

	cfg,err := config.Load()
	if err!=nil{
		log.Fatal(err)
	}

	if err:= database.RunMigrate(cfg.DatabaseUrl);err!=nil && err !=migrate.ErrNoChange{
		log.Fatal(err)
	}

	sqlDB,err:=database.Connect(cfg.DatabaseUrl);
	
	if err!=nil{
		log.Fatal(err)
	}

	h := handler.New(sqlDB)

	app := fiber.New()
	routes.Setup(app,h)

	log.Fatal(app.Listen(":8080"))	
}