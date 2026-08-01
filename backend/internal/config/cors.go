package config

import (
	"github.com/gofiber/fiber/v3/middleware/cors"
)

func CorsConfig() cors.Config {
	return cors.Config{
		AllowOrigins:[] string{
			"http://localhost:3000",
		},
		AllowHeaders: []string{
			"Origin"," Content-Type","Accept","Authorization",
		},
		AllowMethods: []string{
			"GET","POST","PATCH","DELETE","OPTIONS",
		},
	}
}