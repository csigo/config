package config

const (
	// PkgName is for creating module counter
	PkgName = "config"

	cRootDataErr   = "config.root_data.err"
	cGetContentErr = "config.get_content.err"
	cGetProcTime   = "config.get.proc_time"
	cCacheMiss     = "config.cache.miss"
	cListGetFail   = "config.list.get.fail.warn"
)

type dummyStat struct{}

type dummyEnd struct{}

func (d dummyStat) BumpAvg(key string, val float64) {}

func (d dummyStat) BumpSum(key string, val float64) {}

func (d dummyStat) BumpHistogram(key string, val float64) {}

func (d dummyStat) BumpTime(key string) interface {
	End()
} {
	return dummyEnd{}
}

func (d dummyEnd) End() {}
