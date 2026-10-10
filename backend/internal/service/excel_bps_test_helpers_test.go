package service

import (
	"context"
	"net/http"
	"sync"
)

type excelBPSSettingsRepo struct {
	SettingRepository
	values map[string]string
}

func (r *excelBPSSettingsRepo) GetValue(_ context.Context, k string) (string, error) {
	return r.values[k], nil
}
func (r *excelBPSSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	v := map[string]string{}
	for _, k := range keys {
		v[k] = r.values[k]
	}
	return v, nil
}
func (r *excelBPSSettingsRepo) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}
func (r *excelBPSSettingsRepo) SetMultiple(_ context.Context, v map[string]string) error {
	for k, x := range v {
		r.values[k] = x
	}
	return nil
}
func excelBPSTestService(upstream *httpUpstreamRecorder) *OpenAIGatewayService {
	s := openAIClientToolsTestService(upstream)
	s.settingService = &SettingService{settingRepo: &excelBPSSettingsRepo{values: map[string]string{SettingKeyExcelBPSEnabled: "true"}}}
	return s
}

type excelBPSImageSettingsRepo struct {
	SettingRepository
	mu     sync.Mutex
	values map[string]string
	err    error
}

func (r *excelBPSImageSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		values[key] = r.values[key]
	}
	return values, r.err
}

func (r *excelBPSImageSettingsRepo) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, r.err
}

func (r *excelBPSImageSettingsRepo) GetValue(ctx context.Context, key string) (string, error) {
	values, err := r.GetMultiple(ctx, []string{key})
	return values[key], err
}

func (r *excelBPSImageSettingsRepo) SetMultiple(_ context.Context, values map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

type bpsTestUpstream struct {
	httpUpstreamRecorder
	send func(*http.Request, string) (*http.Response, error)
}

func (u *bpsTestUpstream) Do(r *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	return u.send(r, proxy)
}
