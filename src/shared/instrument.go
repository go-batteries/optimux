package shared

import (
	"fmt"
	"log"
	"sync"

	"github.com/DataDog/datadog-go/v5/statsd"
	"github.com/go-batteries/optimux/src/config"
)

var (
	instrumenter statsd.ClientInterface
	env          string
	once         sync.Once
)

const (
	EventAssetFound             = "optimux.asset.200"
	EventAssetNotFound          = "optimux.asset.404"
	EventAssetProcessingFailed  = "optimux.processor.500"
	EventAssetProcessingSuccess = "optimux.processor.201"

	EventOnTheFlyQLen      = "optimux.flyqueue.size"
	EventScalerWorkerCount = "optimux.flyqueue.woker_count"
	EventScalerScaleUp     = "optimux.scaler.up"
	EventScalerScaleDown   = "optimux.scaler.down"
)

var (
	DefaultDDTagsServerStg  = []string{"service:optimux", "env:staging"}
	DefaultDDTagsServerProd = []string{"service:optimux", "env:production"}

	DefaultDDTagsWorkerStg  = []string{"service:energon", "env:staging"}
	DefaultDDTagsWorkerProd = []string{"service:energon", "env:production"}
)

func MustSetupInstrumenter(cfg *config.Config) {
	once.Do(func() {
		var err error

		instrumenter, err = statsd.New(cfg.StatsDAddr)
		if err != nil {
			log.Println(fmt.Errorf("failed to setup datadog for metric collection. error %v", err))
			instrumenter = &statsd.NoOpClient{}
		}

		env = cfg.Env
	})
}

func I() statsd.ClientInterface {
	return instrumenter
}

func GetServerTags() []string {
	if env == "prod" {
		return DefaultDDTagsServerProd
	}

	return DefaultDDTagsServerStg
}

func GetWorkerTags() []string {
	if env == "prod" {
		return DefaultDDTagsWorkerProd
	}

	return DefaultDDTagsWorkerStg
}
