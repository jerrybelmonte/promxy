package promclient

import (
	"context"
	"fmt"
	"time"

	"github.com/jacksontj/promxy/promhttputil"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// MultiAPIMetricFunc defines a method where a client can record metrics about
// the specific API calls made through this multi client
type MultiAPIMetricFunc func(i int, api, status string, took float64)

// NewMultiAPI returns a MultiAPI
func NewMultiAPI(apis []API, antiAffinity model.Time, metricFunc MultiAPIMetricFunc) *MultiAPI {
	return &MultiAPI{
		apis:         apis,
		antiAffinity: antiAffinity,
		metricFunc:   metricFunc,
	}
}

// MultiAPI implements the API interface while merging the results from the apis it wraps
type MultiAPI struct {
	apis         []API
	antiAffinity model.Time
	metricFunc   MultiAPIMetricFunc
}

func (m *MultiAPI) recordMetric(i int, api, status string, took float64) {
	if m.metricFunc != nil {
		m.metricFunc(i, api, status, took)
	}
}

// LabelValues performs a query for the values of the given label.
func (m *MultiAPI) LabelValues(ctx context.Context, label string) (model.LabelValues, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(m.apis))

	for i, api := range m.apis {
		resultChans[i] = make(chan interface{}, 1)
		go func(i int, retChan chan interface{}, api API, label string) {
			start := time.Now()
			result, err := api.LabelValues(childContext, label)
			took := time.Now().Sub(start)
			if err != nil {
				m.recordMetric(i, "label_values", "error", took.Seconds())
				retChan <- err
			} else {
				m.recordMetric(i, "label_values", "success", took.Seconds())
				retChan <- result
			}
		}(i, resultChans[i], api, label)
	}

	// Wait for results as we get them
	var result []model.LabelValue
	var lastError error
	errCount := 0
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ret := <-resultChans[i]:
			switch retTyped := ret.(type) {
			case error:
				lastError = retTyped
				errCount++
			case model.LabelValues:
				if result == nil {
					result = retTyped
				} else {
					result = MergeLabelValues(result, retTyped)
				}
			default:
				return nil, fmt.Errorf("Unknown return type")
			}
		}
	}

	// If we got only errors, lets return that
	if errCount == len(m.apis) {
		return nil, errors.Wrap(lastError, "Unable to fetch from downstream servers")
	}

	return result, nil
}

// Query performs a query for the given time.
func (m *MultiAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(m.apis))

	for i, api := range m.apis {
		resultChans[i] = make(chan interface{}, 1)
		go func(i int, retChan chan interface{}, api API, query string, ts time.Time) {
			start := time.Now()
			result, err := api.Query(childContext, query, ts)
			took := time.Now().Sub(start)
			if err != nil {
				m.recordMetric(i, "query", "error", took.Seconds())
				retChan <- err
			} else {
				m.recordMetric(i, "query", "success", took.Seconds())
				retChan <- result
			}
		}(i, resultChans[i], api, query, ts)
	}

	// Wait for results as we get them
	var result model.Value
	var lastError error
	errCount := 0
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case ret := <-resultChans[i]:
			switch retTyped := ret.(type) {
			case error:
				lastError = retTyped
				errCount++
			case model.Value:
				// TODO: check qData.ResultType
				if result == nil {
					result = retTyped
				} else {
					var err error
					result, err = promhttputil.MergeValues(m.antiAffinity, result, retTyped)
					if err != nil {
						return nil, err
					}
				}
			case nil:
				continue
			default:
				return nil, fmt.Errorf("Unknown return type")
			}
		}
	}

	if errCount != 0 && errCount == len(m.apis) {
		return nil, errors.Wrap(lastError, "Unable to fetch from downstream servers")
	}

	return result, nil
}

// QueryRange performs a query for the given range.
func (m *MultiAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(m.apis))

	for i, api := range m.apis {
		resultChans[i] = make(chan interface{}, 1)
		go func(i int, retChan chan interface{}, api API, query string, r v1.Range) {
			start := time.Now()
			result, err := api.QueryRange(childContext, query, r)
			took := time.Now().Sub(start)
			if err != nil {
				m.recordMetric(i, "query_range", "error", took.Seconds())
				retChan <- err
			} else {
				m.recordMetric(i, "query_range", "success", took.Seconds())
				retChan <- result
			}
		}(i, resultChans[i], api, query, r)
	}

	// Wait for results as we get them
	var result model.Value
	var lastError error
	errCount := 0
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case ret := <-resultChans[i]:
			switch retTyped := ret.(type) {
			case error:
				lastError = retTyped
				errCount++
			case model.Value:
				// TODO: check qData.ResultType
				if result == nil {
					result = retTyped
				} else {
					var err error
					result, err = promhttputil.MergeValues(m.antiAffinity, result, retTyped)
					if err != nil {
						return nil, err
					}
				}
			case nil:
				continue
			default:
				return nil, fmt.Errorf("Unknown return type")
			}
		}
	}

	if errCount != 0 && errCount == len(m.apis) {
		return nil, errors.Wrap(lastError, "Unable to fetch from downstream servers")
	}

	return result, nil
}

// Series finds series by label matchers.
func (m *MultiAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(m.apis))

	for i, api := range m.apis {
		resultChans[i] = make(chan interface{}, 1)
		go func(i int, retChan chan interface{}, api API) {
			start := time.Now()
			result, err := api.Series(childContext, matches, startTime, endTime)
			took := time.Now().Sub(start)
			if err != nil {
				m.recordMetric(i, "series", "error", took.Seconds())
				retChan <- err
			} else {
				m.recordMetric(i, "series", "success", took.Seconds())
				retChan <- result
			}
		}(i, resultChans[i], api)
	}

	// Wait for results as we get them
	var result []model.LabelSet
	var lastError error
	errCount := 0
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ret := <-resultChans[i]:
			switch retTyped := ret.(type) {
			case error:
				lastError = retTyped
				errCount++
			case []model.LabelSet:
				if result == nil {
					result = retTyped
				} else {
					result = MergeLabelSets(result, retTyped)
				}
			default:
				return nil, fmt.Errorf("Unknown return type")
			}
		}
	}

	// If we got only errors, lets return that
	if errCount == len(m.apis) {
		return nil, errors.Wrap(lastError, "Unable to fetch from downstream servers")
	}

	return result, nil
}