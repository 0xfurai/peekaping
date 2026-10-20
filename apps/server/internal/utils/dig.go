package utils

import (
	"fmt"
	"peekaping/internal/config"
	"reflect"
	"runtime"

	"go.uber.org/dig"
)

// Provide registers the constructors in the container, in order. It stops at the first one
// dig refuses, for example because another constructor already provides its type, and returns
// dig's error with that constructor's name. Every provide goes through it, so a refused one
// stops startup instead of being dropped.
func Provide(container *dig.Container, constructors ...interface{}) error {
	for _, constructor := range constructors {
		if err := container.Provide(constructor); err != nil {
			return fmt.Errorf("failed to provide %s: %w", constructorName(constructor), err)
		}
	}
	return nil
}

// constructorName is the constructor's function name, such as
// peekaping/internal/modules/heartbeat.NewService, or the type of a value that isn't a function
func constructorName(constructor interface{}) string {
	value := reflect.ValueOf(constructor)
	if value.Kind() != reflect.Func {
		return fmt.Sprintf("%T", constructor)
	}
	if fn := runtime.FuncForPC(value.Pointer()); fn != nil {
		return fn.Name()
	}
	return value.Type().String()
}

// RegisterRepositoryByDBType registers the appropriate repository based on database type
// sqlRepoConstructor: constructor function for SQL repositories (e.g., NewSQLRepository)
// mongoRepoConstructor: constructor function for MongoDB repositories (e.g., NewMongoRepository)
func RegisterRepositoryByDBType(container *dig.Container, cfg *config.Config, sqlRepoConstructor, mongoRepoConstructor interface{}) error {
	switch cfg.DBType {
	case "postgres", "postgresql", "mysql", "sqlite":
		return Provide(container, sqlRepoConstructor)
	case "mongo":
		return Provide(container, mongoRepoConstructor)
	}
	return nil
}
