package database

import (
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func RunMigrate(dsn string) error{
	m,err :=migrate.New("file://migrations",dsn)
	if err!=nil{
		return  err
	}
	return m.Up()
}