package config

import (
	"flag"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Domain           url.URL
	PgURL            *url.URL
	Port             string
	LogFile          string
	S3BaseURL        string
	SourceAPIBaseURL string
	Origins          string
	Env              string
	StatsDAddr       string
	DefaultS3Bucket  string
	AwsRegion        string

	Quality         int
	MaxPrefetch     int
	VipsConcurrency int32
	QSize           int32

	UseHttp2, DoProfile bool
}

const (
	DefaultQSize         = 10000 // 10K
	DefaultImageQuality  = 100
	DefaultPrefetchCount = 50
)

const DefaultAwsRegion = "us-east-1"

const (
	DefaultStatsDAddr = "127.0.0.1:8125"
)

type ConfigLoaderOpts func(c *Config)

func LoadFromCliArgs(opts ...ConfigLoaderOpts) *Config {
	config := &Config{
		VipsConcurrency: 20,
		MaxPrefetch:     DefaultPrefetchCount,
	}

	flag.StringVar(&config.Port, "port", ":8811", "port number")
	flag.IntVar(&config.Quality, "quality", DefaultImageQuality, "image quality")
	flag.BoolVar(&config.UseHttp2, "h2", false, "use http2")
	flag.BoolVar(&config.DoProfile, "pprof", false, "enable profiling apis")
	flag.StringVar(&config.LogFile, "log-file", "", "pass a file to log to")

	flag.Parse()

	for _, opt := range opts {
		opt(config)
	}

	return config
}

var envOverrides = map[string]string{
	"local":      "local",
	"dev":        "stg",
	"stg":        "stg",
	"staging":    "stg",
	"prod":       "prod",
	"production": "prod",
}

func WithEnvConfigLoaderOpts() ConfigLoaderOpts {
	return func(config *Config) {
		env := strings.ToLower(strings.TrimSpace(os.Getenv("ENVIRONMENT")))

		log.Printf("Using ENV '%s'", env)

		if env == "" {
			env = "prod"
		}

		if oenv, ok := envOverrides[env]; ok {
			env = oenv
		}

		log.Printf("ENV after override '%s'", env)

		origins := strings.TrimSpace(os.Getenv("ORIGINS"))
		if origins == "" {
			log.Fatal("ORIGINS is required.")
		}

		s3BaseUrl := strings.TrimRight(strings.TrimSpace(os.Getenv("S3_BASE_URL")), "/")
		if s3BaseUrl == "" {
			log.Fatal("S3_BASE_URL not set in env")
		}

		_, err := url.Parse(s3BaseUrl)
		if err != nil {
			log.Fatal("invalid url provided", err)
		}

		sourceAPIBaseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("SOURCE_API_BASE_URL")), "/")
		if sourceAPIBaseURL == "" {
			sourceAPIBaseURL = "http://localhost:8812"
		}
		config.SourceAPIBaseURL = sourceAPIBaseURL

		qSizeStr := strings.TrimSpace(os.Getenv("QSIZE"))
		qsize := DefaultQSize

		qsizeInt, err := strconv.Atoi(qSizeStr)
		if err == nil {
			qsize = qsizeInt
		} else {
			log.Println("Invalid qsize", qSizeStr, "using default")
		}

		domainStr := strings.TrimSpace(os.Getenv("DOMAIN"))
		if domainStr == "" {
			log.Fatal("domain name is empty")
		}

		domain, err := url.Parse(domainStr)
		if err != nil {
			log.Fatalf("invalid domain name %s. error %v", domain, err)
		}

		pgURLStr := strings.TrimSpace(os.Getenv("PG_URL"))
		pgURL, err := url.Parse(pgURLStr)
		if err != nil {
			log.Fatalf("failed to parse postgres url. error %v", err)
		}

		statsDPort := strings.TrimSpace(os.Getenv("STATSD_ADDR"))
		if statsDPort == "" {
			statsDPort = DefaultStatsDAddr
		}

		awsRegion := strings.TrimSpace(os.Getenv("AWS_REGION"))
		if awsRegion == "" {
			awsRegion = DefaultAwsRegion
		}

		hostSplits := strings.Split(s3BaseUrl, ".s3.amazonaws.com")
		if len(hostSplits) < 1 {
			log.Fatal("should not have failed to extract bucket from s3 base url")
		}

		config.DefaultS3Bucket = hostSplits[0]
		config.S3BaseURL = s3BaseUrl
		config.Origins = origins
		config.Env = env
		config.Domain = *domain
		config.QSize = int32(qsize)
		config.PgURL = pgURL
		config.StatsDAddr = statsDPort
		config.AwsRegion = awsRegion

		vipsConcurrency := strings.TrimSpace(os.Getenv("VIPS_PROCS"))
		if vipsConcurrency == "" {
			return
		}

		res, err := strconv.Atoi(vipsConcurrency)
		if err != nil {
			return
		}

		config.VipsConcurrency = int32(res)
	}
}

func (c *Config) IsEnvLocal() bool {
	env := c.Env

	return env == "local" || env == "dev"
}

// type PgConfig struct {
// 	User     string
// 	Password string
// 	Host     string
// 	Port     string
// 	DbName   string
// 	Query    url.Values
// }
//
// func ParsePgConfigFromUrl(pgURL *url.URL) *PgConfig {
// 	pg := &PgConfig{
// 		Host:   pgURL.Hostname(),
// 		Port:   pgURL.Port(),
// 		DbName: pgURL.Path,
// 		Query:  pgURL.Query(),
// 	}
//
// 	pg.User = pgURL.User.Username()
// 	password, ok := pgURL.User.Password()
// 	if ok {
// 		pg.Password = password
// 	}
//
// 	return pg
// }
