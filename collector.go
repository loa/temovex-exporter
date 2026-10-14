package main

import (
	"math"

	"github.com/loa/temovex-exporter/temovex"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

type collector struct {
	Temovex *temovex.Client

	tempMetric *prometheus.Desc
}

func newCollector(address string) *collector {
	col := collector{
		tempMetric: prometheus.NewDesc(
			"temovex_temperature_c",
			"Temperature in celcius",
			[]string{"name"},
			nil,
		),
	}

	var err error
	col.Temovex, err = temovex.NewClient(address)
	if err != nil {
		log.Fatal(err.Error())
	}

	return &col
}

func (col *collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- col.tempMetric
}

func (col *collector) Collect(ch chan<- prometheus.Metric) {
	if val, err := col.Temovex.GetSet(); err != nil {
		log.Fatal(err.Error())
	} else {
		ch <- prometheus.MustNewConstMetric(
			col.tempMetric,
			prometheus.GaugeValue,
			math.Round(val*10)/10,
			"set",
		)
	}

	if val, err := col.Temovex.GetAL(); err != nil {
		log.Fatal(err.Error())
	} else {
		ch <- prometheus.MustNewConstMetric(
			col.tempMetric,
			prometheus.GaugeValue,
			math.Round(val*10)/10,
			"al",
		)
	}

	if val, err := col.Temovex.GetFL(); err != nil {
		log.Fatal(err.Error())
	} else {
		ch <- prometheus.MustNewConstMetric(
			col.tempMetric,
			prometheus.GaugeValue,
			math.Round(val*10)/10,
			"fl",
		)
	}

	if val, err := col.Temovex.GetUL(); err != nil {
		log.Fatal(err.Error())
	} else {
		ch <- prometheus.MustNewConstMetric(
			col.tempMetric,
			prometheus.GaugeValue,
			math.Round(val*10)/10,
			"ul",
		)
	}

	if val, err := col.Temovex.GetTL(); err != nil {
		log.Fatal(err.Error())
	} else {
		ch <- prometheus.MustNewConstMetric(
			col.tempMetric,
			prometheus.GaugeValue,
			math.Round(val*10)/10,
			"tl",
		)
	}
}
