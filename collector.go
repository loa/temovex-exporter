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
	if val, err := col.Temovex.GetDesired(); err != nil {
		log.Fatal(err.Error())
	} else {
		ch <- prometheus.MustNewConstMetric(
			col.tempMetric,
			prometheus.GaugeValue,
			math.Round(val*10)/10,
			"desired",
		)
	}

	if val, err := col.Temovex.GetExhaust(); err != nil {
		log.Fatal(err.Error())
	} else {
		ch <- prometheus.MustNewConstMetric(
			col.tempMetric,
			prometheus.GaugeValue,
			math.Round(val*10)/10,
			"exhaust",
		)
	}

	if val, err := col.Temovex.GetExtract(); err != nil {
		log.Fatal(err.Error())
	} else {
		ch <- prometheus.MustNewConstMetric(
			col.tempMetric,
			prometheus.GaugeValue,
			math.Round(val*10)/10,
			"extract",
		)
	}

	if val, err := col.Temovex.GetOutdoor(); err != nil {
		log.Fatal(err.Error())
	} else {
		ch <- prometheus.MustNewConstMetric(
			col.tempMetric,
			prometheus.GaugeValue,
			math.Round(val*10)/10,
			"outdoor",
		)
	}

	if val, err := col.Temovex.GetSupply(); err != nil {
		log.Fatal(err.Error())
	} else {
		ch <- prometheus.MustNewConstMetric(
			col.tempMetric,
			prometheus.GaugeValue,
			math.Round(val*10)/10,
			"supply",
		)
	}
}
