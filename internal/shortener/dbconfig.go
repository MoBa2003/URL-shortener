package shortener

import "fmt"

type DBConfig struct {
	Host     string
	User     string
	Password string
	DBName   string
	Port     string
	SSLMode  string
}

func NewDBConfig(host string, user string, password string, dbname string, port string, sslmode string) *DBConfig {
	return &DBConfig{Host: host, User: user, Password: password, DBName: dbname, Port: port, SSLMode: sslmode}
}

func (config *DBConfig) GetFormattedString() string {
	return fmt.Sprintf("host=%v user=%v password=%v dbname=%v port=%v sslmode=%v", config.Host, config.User, config.Password, config.DBName, config.Port, config.SSLMode)
}
