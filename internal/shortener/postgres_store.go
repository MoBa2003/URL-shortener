package shortener

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type URLModel struct {
	Code      string    `gorm:"primaryKey;type:varchar(10)"`
	LongURL   string    `gorm:"uniqueIndex;type:text;not null"`
	CreatedAt time.Time `gorm:"not null"`
}

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})

	if err != nil {
		return nil, fmt.Errorf("failed to connecto to postgres : %w", err)
	}

	if err := db.AutoMigrate(&URLModel{}); err != nil {
		return nil, fmt.Errorf("failed to migrate data base scheme : %w", err)
	}
	return &PostgresStore{db: db}, nil
}

func (p *PostgresStore) Shorten(rawurl string) (string, error) {
	normalizedurl, err := NormalizeURL(rawurl)
	if err != nil {
		return "", err
	}

	var existing URLModel
	result := p.db.Where("long_url = ?", normalizedurl).First(&existing)
	if result.Error == nil {
		return existing.Code, nil
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("database query error: %w", result.Error)
	}

	const maxtries = 10
	for i := 0; i < maxtries; i++ {
		code, err := GenerateCode(6)
		if err != nil {
			return "", err
		}

		newRecord := URLModel{
			Code:      code,
			LongURL:   normalizedurl,
			CreatedAt: time.Now().UTC(),
		}
		myerr := p.db.Create(&newRecord).Error
		if myerr == nil {
			return code, nil
		}

		var concurrentRecord URLModel
		checkErr := p.db.Where("long_url = ?", normalizedurl).First(&concurrentRecord).Error
		if checkErr == nil {
			return concurrentRecord.Code, nil
		}

	}
	return "", fmt.Errorf("failed to generate shortcode")
}

func (p *PostgresStore) GetMetadatafromCode(code string) (MetaData, error) {
	var record URLModel
	result := p.db.First(&record, "code = ?", code)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return MetaData{}, NotFoundErr
		}
		return MetaData{}, fmt.Errorf("database query error: %w", result.Error)
	}
	return MetaData{
		Longurl:   record.LongURL,
		CreatedAt: record.CreatedAt,
	}, nil
}
