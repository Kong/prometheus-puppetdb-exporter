package exporter

import (
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"

	"github.com/camptocamp/prometheus-puppetdb-exporter/internal/puppetdb"
)

// ExporterConfig defines the config for Exporter
type Config struct {
	URL        string
	CertPath   string
	CACertPath string
	KeyPath    string
	SSLVerify  bool

	Categories         map[string]struct{}
	UnreportedDuration time.Duration
}

// Exporter implements the prometheus.Exporter interface, and exports PuppetDB metrics
type Exporter struct {
	client             *puppetdb.PuppetDB
	namespace          string
	metrics            map[string]*prometheus.Desc
	categories         map[string]struct{}
	unreportedDuration time.Duration
}

var (
	metricMap = map[string]string{
		"node_status_count": "node_status_count",
	}
)

// NewPuppetDBExporter returns a new exporter of PuppetDB metrics.
func NewPuppetDBExporter(c *Config, r *prometheus.Registry) (e *Exporter, err error) {
	e = &Exporter{
		namespace:          "puppetdb",
		categories:         c.Categories,
		unreportedDuration: c.UnreportedDuration,
	}

	opts := &puppetdb.Options{
		URL:        c.URL,
		CertPath:   c.CertPath,
		CACertPath: c.CACertPath,
		KeyPath:    c.KeyPath,
		SSLVerify:  c.SSLVerify,
	}

	e.client, err = puppetdb.NewClient(opts)
	if err != nil {
		log.Fatalf("failed to create new client: %s", err)
		return
	}

	e.initGauges(c.Categories)
	r.MustRegister(e)

	return
}

// Describe outputs PuppetDB metric descriptions
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range e.metrics {
		ch <- m
	}
}

// Collect fetches new metrics from the PuppetDB and updates the appropriate metrics
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	statuses := make(map[string]int)

	collectStart := time.Now()

	nodes, err := e.client.Nodes()
	if err != nil {
		for _, m := range e.metrics {
			ch <- prometheus.NewInvalidMetric(m, err)
		}
		return
	}

	for _, node := range nodes {
		var deactivated string
		if node.Deactivated == "" {
			deactivated = "false"
		} else {
			deactivated = "true"
		}

		if node.ReportTimestamp == "" {
			statuses["unreported"]++
			continue
		}
		latestReport, err := time.Parse("2006-01-02T15:04:05Z", node.ReportTimestamp)
		if err != nil {
			log.Errorf("failed to parse report timestamp: %s", err)
			continue
		}

		ch <- prometheus.MustNewConstMetric(
			e.metrics["report"], prometheus.GaugeValue,
			float64(latestReport.Unix()),
			node.ReportEnvironment, node.Certname, deactivated)

		if latestReport.Add(e.unreportedDuration).Before(time.Now()) {
			statuses["unreported"]++
		}

		lastReportStatus := node.LatestReportStatus
		if lastReportStatus == "" {
			lastReportStatus = "unreported"
		}

		statuses[lastReportStatus]++

		ch <- prometheus.MustNewConstMetric(
			e.metrics["node_last_report_status"], prometheus.GaugeValue,
			1,
			lastReportStatus, node.Certname,
		)

		if node.LatestReportHash != "" {
			reportMetrics, _ := e.client.ReportMetrics(node.LatestReportHash)
			for _, reportMetric := range reportMetrics {
				_, ok := e.categories[reportMetric.Category]
				if ok {
					category := fmt.Sprintf("report_%s", reportMetric.Category)
					ch <- prometheus.MustNewConstMetric(
						e.metrics[category], prometheus.GaugeValue,
						reportMetric.Value,
						strings.ReplaceAll(strings.Title(reportMetric.Name), "_", " "),
						node.ReportEnvironment,
						node.Certname)
				}
			}
		}
	}

	for statusName, statusValue := range statuses {
		ch <- prometheus.MustNewConstMetric(
			e.metrics["node_report_status_count"], prometheus.GaugeValue,
			float64(statusValue),
			statusName)
	}

	duration := 1000000 * time.Now().Sub(collectStart).Nanoseconds()
	ch <- prometheus.MustNewConstMetric(
		e.metrics["puppetdb_exporter_collect_duration"], prometheus.GaugeValue,
		float64(duration))
}

func (e *Exporter) initGauges(categories map[string]struct{}) {
	e.metrics = map[string]*prometheus.Desc{}

	e.metrics["node_last_report_status"] = prometheus.NewDesc(
		e.namespace+"_node_last_report_status",
		"Last report status for a node by type",
		[]string{"status", "host"},
		nil,
	)

	e.metrics["node_report_status_count"] = prometheus.NewDesc(
		e.namespace+"node_report_status_count",
		"Total count of reports status by type",
		[]string{"status"},
		nil)

	for category := range categories {
		metricName := fmt.Sprintf("report_%s", category)
		e.metrics[metricName] = prometheus.NewDesc(
			"puppet_"+metricName,
			fmt.Sprintf("Total count of %s per status", category),
			[]string{"name", "environment", "host"},
			nil)

	}

	e.metrics["report"] = prometheus.NewDesc(
		"puppet_report",
		"Timestamp of latest report",
		[]string{"environment", "host", "deactivated"},
		nil)

	e.metrics["puppetdb_exporter_collect_duration"] = prometheus.NewDesc(
		"puppetdb_exporter_collect_duration",
		"Time taken to talk to puppetdb and generate metrics in milliseconds",
		[]string{},
		nil)

}
