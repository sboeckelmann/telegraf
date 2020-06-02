package models

import (
	"sync"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/selfstat"
)

type RunningProcessor struct {
	sync.Mutex
	log       telegraf.Logger
	Processor telegraf.StreamingProcessor
	Config    *ProcessorConfig
}

type RunningProcessors []*RunningProcessor

func (rp RunningProcessors) Len() int           { return len(rp) }
func (rp RunningProcessors) Swap(i, j int)      { rp[i], rp[j] = rp[j], rp[i] }
func (rp RunningProcessors) Less(i, j int) bool { return rp[i].Config.Order < rp[j].Config.Order }

// FilterConfig containing a name and filter
type ProcessorConfig struct {
	Name   string
	Alias  string
	Order  int64
	Filter Filter
}

func NewRunningProcessor(processor telegraf.StreamingProcessor, config *ProcessorConfig) *RunningProcessor {
	tags := map[string]string{"processor": config.Name}
	if config.Alias != "" {
		tags["alias"] = config.Alias
	}

	processErrorsRegister := selfstat.Register("process", "errors", tags)
	logger := NewLogger("processors", config.Name, config.Alias)
	logger.OnErr(func() {
		processErrorsRegister.Incr(1)
	})
	setLogIfExist(processor, logger)

	return &RunningProcessor{
		Processor: processor,
		Config:    config,
		log:       logger,
	}
}

func (rp *RunningProcessor) metricFiltered(metric telegraf.Metric) {
	metric.Drop()
}

func containsMetric(item telegraf.Metric, metrics []telegraf.Metric) bool {
	for _, m := range metrics {
		if item == m {
			return true
		}
	}
	return false
}

func (r *RunningProcessor) Init() error {
	if p, ok := r.Processor.(telegraf.Initializer); ok {
		err := p.Init()
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RunningProcessor) Log() telegraf.Logger {
	return r.log
}

func (r *RunningProcessor) Start(acc telegraf.MetricStreamAccumulator) error {
	return r.Processor.Start(acc)
}

func (r *RunningProcessor) Run(in <-chan telegraf.Metric, acc telegraf.MetricStreamAccumulator) {
	for m := range in {
		if ok := r.Config.Filter.Select(m); !ok {
			// pass downstream
			acc.PassMetric(m)
			continue
		}

		r.Config.Filter.Modify(m)
		if len(m.FieldList()) == 0 {
			// drop metric
			r.metricFiltered(m)
			continue
		}

		r.Processor.Add(m)
	}
	r.Processor.Stop()
}
