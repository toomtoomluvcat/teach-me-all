package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct{ 
	DatabaseUrl string
	AppPort string
}

func Load() (*Config,error){
	godotenv.Load(".env")
	
        if _,err:=os.Stat(".env");os.IsNotExist(err){
		godotenv.Load("../../.env")
        }
        user := os.Getenv("DB_USER")
        password := os.Getenv("DB_PASSWORD")
        host := os.Getenv("DB_HOST")
        port := os.Getenv("DB_PORT")
        dbName := os.Getenv("DB_NAME")
        appPort := os.Getenv("APP_PORT")

        if user == "" || password == "" || host == "" || port == "" || dbName == "" {
                return nil, fmt.Errorf("missing required environment variables")
        }
        dsn:=fmt.Sprintf(
                "postgres://%s:%s@%s:%s/%s?sslmode=disable",
                user,
                password,
                host,
                port,
                dbName,
        )
        return &Config{
                DatabaseUrl: dsn,
                AppPort: appPort,
        },nil
}