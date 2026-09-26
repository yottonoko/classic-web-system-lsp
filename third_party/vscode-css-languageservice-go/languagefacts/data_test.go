package languagefacts

import "testing"

var dataManagerBenchmarkSink *DataManager

func TestDefaultDataManagerProperties(t *testing.T) {
	t.Run("properties", func(t *testing.T) {
		manager := NewDataManager(DataManagerOptions{})
		property, ok := manager.GetProperty("text-decoration-color")
		if !ok {
			t.Fatal("text-decoration-color not found")
		}
		if property.Name != "text-decoration-color" {
			t.Fatalf("name = %q", property.Name)
		}
		if property.Baseline == nil || property.Baseline.Status != BaselineHigh {
			t.Fatalf("baseline = %#v", property.Baseline)
		}
		for _, browser := range []string{"E79", "FF36", "C57", "S12.1", "O44"} {
			if !contains(property.Browsers, browser) {
				t.Fatalf("browser %q not found in %#v", browser, property.Browsers)
			}
		}
		if missing := GetMissingBaselineBrowsers(property.Browsers); missing != "" {
			t.Fatalf("missing baseline browsers = %q", missing)
		}
		if len(property.Restrictions) != 1 || property.Restrictions[0] != "color" {
			t.Fatalf("restrictions = %#v", property.Restrictions)
		}
		if !manager.IsKnownProperty("TEXT-DECORATION-COLOR") {
			t.Fatal("property lookup should be case-insensitive")
		}
	})
}

func TestDataProviderCounts(t *testing.T) {
	provider := GetDefaultCSSDataProvider()
	if got := len(provider.ProvideProperties()); got != 888 {
		t.Fatalf("properties = %d", got)
	}
	if got := len(provider.ProvideAtDirectives()); got != 25 {
		t.Fatalf("atDirectives = %d", got)
	}
	if got := len(provider.ProvidePseudoClasses()); got != 111 {
		t.Fatalf("pseudoClasses = %d", got)
	}
	if got := len(provider.ProvidePseudoElements()); got != 90 {
		t.Fatalf("pseudoElements = %d", got)
	}
}

func TestDataManagerSetDataProvidersReplacesCollectedData(t *testing.T) {
	manager := NewDataManager(DataManagerOptions{})
	untouchedManager := NewDataManager(DataManagerOptions{})
	if len(manager.GetProperties()) == 0 {
		t.Fatal("expected default properties")
	}

	custom := NewCSSDataProvider(CSSDataV1{
		Version:    1.1,
		Properties: []PropertyData{{Name: "custom-prop"}},
	})
	manager.SetDataProviders(false, []CSSDataProvider{custom})

	properties := manager.GetProperties()
	if len(properties) != 1 || properties[0].Name != "custom-prop" {
		t.Fatalf("properties = %#v", properties)
	}
	if manager.IsKnownProperty("text-decoration-color") {
		t.Fatal("old default property should not remain after provider reset")
	}
	if !untouchedManager.IsKnownProperty("text-decoration-color") {
		t.Fatal("custom providers should not change another manager's default data")
	}
}

func BenchmarkNewDefaultDataManager(b *testing.B) {
	for b.Loop() {
		dataManagerBenchmarkSink = NewDataManager(DataManagerOptions{})
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
