package config

// Config holds the service configuration.
type Config struct {
	ServerHost   string
	ServerPort   int
	LogLevel     string
	DatabaseURL  string // internal PostgreSQL for mapping store
	MySQLRootURL string
	MySQLHost    string
	MySQLPort    int
	MongoRootURL string
	MongoHost    string
	MongoPort    int
}
