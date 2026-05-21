package config

import (
	"fmt"
	"os"
	"strconv"
)

// Load reads environment variables and returns a Config.
func Load() (*Config, error) {
	serverPort := 3500
	if p := os.Getenv("SERVER_PORT"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil {
			serverPort = parsed
		}
	}

	mysqlPort := 3306
	if p := os.Getenv("MYSQL_PORT"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil {
			mysqlPort = parsed
		}
	}

	mongoPort := 27017
	if p := os.Getenv("MONGO_PORT"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil {
			mongoPort = parsed
		}
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	return &Config{
		ServerHost:   getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:   serverPort,
		LogLevel:     getEnv("LOG_LEVEL", "info"),
		DatabaseURL:  databaseURL,
		MySQLRootURL: getEnv("MYSQL_ROOT_URL", "root:root@tcp(db_mysql:3306)/"),
		MySQLHost:    getEnv("MYSQL_HOST", "db_mysql"),
		MySQLPort:    mysqlPort,
		MongoRootURL: getEnv("MONGO_ROOT_URL", "mongodb://root:root@db_mongo:27017"),
		MongoHost:    getEnv("MONGO_HOST", "db_mongo"),
		MongoPort:    mongoPort,
	}, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
