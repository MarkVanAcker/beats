// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package ccr

import (
	"fmt"
	"time"

	"github.com/elastic/beats/v7/metricbeat/helper/elastic"
	"github.com/elastic/beats/v7/metricbeat/mb"
	"github.com/elastic/beats/v7/metricbeat/module/elasticsearch"
	"github.com/elastic/elastic-agent-libs/version"
)

func init() {
	mb.Registry.MustAddMetricSet(elasticsearch.ModuleName, "ccr", New,
		mb.WithHostParser(elasticsearch.HostParser),
	)
}

const (
	ccrStatsPath = "/_ccr/stats"
)

// MetricSet type defines all fields of the MetricSet
type MetricSet struct {
	*elasticsearch.MetricSet
	lastCCRLicenseMessageTimestamp time.Time
}

// New create a new instance of the MetricSet
func New(base mb.BaseMetricSet) (mb.MetricSet, error) {
	ms, err := elasticsearch.NewMetricSet(base, ccrStatsPath)
	if err != nil {
		return nil, err
	}
	return &MetricSet{MetricSet: ms}, nil
}

// Fetch gathers stats for each follower shard from the _ccr/stats API
func (m *MetricSet) Fetch(r mb.ReporterV2) error {
	shouldSkip, err := m.ShouldSkipFetch()
	if err != nil {
		return err
	}
	if shouldSkip {
		return nil
	}

	info, err := elasticsearch.GetInfo(m.HTTP, m.GetServiceURI())
	if err != nil {
		return err
	}

	ccrUnavailableMessage, err := m.checkCCRAvailability(info.Version.Number)
	if err != nil {
		return fmt.Errorf("error determining if CCR is available: %w", err)
	}

	if ccrUnavailableMessage != "" {
		if time.Since(m.lastCCRLicenseMessageTimestamp) > 1*time.Minute {
			m.lastCCRLicenseMessageTimestamp = time.Now()
			m.Logger().Warn(ccrUnavailableMessage)
		}
		return nil
	}

	content, err := m.HTTP.FetchContent()
	if err != nil {
		return err
	}

	return eventsMapping(r, *info, content, m.XPackEnabled)
}

func (m *MetricSet) checkCCRAvailability(currentElasticsearchVersion *version.V) (message string, err error) {
	license, err := elasticsearch.GetLicense(m.HTTP, m.GetServiceURI())
	if err != nil {
		return "", fmt.Errorf("error determining Elasticsearch license: %w", err)
	}

	if !license.IsOneOf("trial", "platinum", "enterprise") {
		message = "the CCR feature is available with a platinum or enterprise Elasticsearch license. " +
			"You currently have a " + license.Type + " license. " +
			"Either upgrade your license or remove the ccr metricset from your Elasticsearch module configuration."
		return
	}

	xpack, err := elasticsearch.GetXPack(m.HTTP, m.GetServiceURI())
	if err != nil {
		return "", fmt.Errorf("error determining xpack features: %w", err)
	}

	if !xpack.Features.CCR.Enabled {
		message = "the CCR feature is not enabled on your Elasticsearch cluster."
		return
	}

	isAvailable := elastic.IsFeatureAvailable(currentElasticsearchVersion, elasticsearch.CCRStatsAPIAvailableVersion)

	if !isAvailable {
		metricsetName := m.FullyQualifiedName()
		message = "the " + metricsetName + " is only supported with Elasticsearch >= " +
			elasticsearch.CCRStatsAPIAvailableVersion.String() + ". " +
			"You are currently running Elasticsearch " + currentElasticsearchVersion.String() + "."
		return
	}

	return "", nil
}
