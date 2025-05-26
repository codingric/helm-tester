package helm_tester

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// func TestHelm(t *testing.T) {
// 	ht := NewHelmTester("./helm")

// 	m, err := ht.Render()
// 	t.Run("render", func(tt *testing.T) {
// 		tt.Run("no-errors", func(ttt *testing.T) {
// 			assert.NoError(ttt, err)
// 			assert.NotEmpty(ttt, m)
// 		})
// 		tt.Run("files", func(ttt *testing.T) {
// 			assert.Contains(ttt, m, "test-chart/charts/echo-server/templates/deployment.yaml")
// 		})

// 		tt.Run("value-updates", func(ttt *testing.T) {
// 			var deploy any

// 			err := yaml.Unmarshal([]byte(m["test-chart/charts/echo-server/templates/deployment.yaml"]), &deploy)
// 			if err != nil {
// 				log.Fatalf("Error unmarshaling YAML: %v", err)
// 			}
// 			image := ht.JQValues(`.Dependencies[0].image.repository`)
// 			rendered, _ := _query(".spec.template.spec.containers[0].image", deploy)
// 			assert.Equal(ttt, rendered.(string), image, "image not updated")
// 		})
// 	})
// }

func TestQuery(t *testing.T) {
	ht := NewHelmTester("./helm")

	t.Run("values", func(tt *testing.T) {
		ht.AssertQueryTrue(tt, `.Chart.Values|keys|contains(["echo-server"])`, "Chart values not present")
	})
	t.Run("dependency-values", func(tt *testing.T) {
		ht.AssertQueryTrue(tt, `.Dependencies[1].Values.image.tag == "0.6.0"`, "Dependencies values not present")
	})
	t.Run("manifests", func(tt *testing.T) {
		ht.AssertQueryTrue(tt, `.Manifests|length == 75`, "Missing manifests")
	})
	t.Run("yq.blank", func(tt *testing.T) {
		v, e := ht.YQ(nil, `.Dependencies[1].Values.image.tag`)
		if assert.NoError(tt, e) {
			assert.Equal(tt, "0.6.0", v)
		}
	})

	t.Run("yq.string", func(tt *testing.T) {
		data := `
str: "string value"
int: 16
bool: true
list:
- one
- 2`
		v, e := ht.YQ(data, ".str")
		if assert.NoError(tt, e) {
			assert.IsType(tt, "string value", v)
			assert.Equal(tt, "string value", v)
		}

		v, e = ht.YQ(data, ".list")
		if assert.NoError(tt, e) {
			x := []any{"one", 2}
			assert.IsType(tt, x, v)
			assert.Equal(tt, x, v)
		}
		v, e = ht.YQ(data, ".bool")
		if assert.NoError(tt, e) {
			x := true
			assert.IsType(tt, x, v)
			assert.Equal(tt, x, v)
		}
		v, e = ht.YQ(data, ".int")
		if assert.NoError(tt, e) {
			x := 16
			assert.IsType(tt, x, v)
			assert.Equal(tt, x, v)
		}

		v, e = ht.YQ(data, ".nonexistant")
		if assert.NoError(tt, e) {
			assert.Nil(tt, v)
		}
	})

	t.Run("yq.any", func(tt *testing.T) {
		data := map[string]any{"string": "string_value", "bool": true, "int": 16, "list": []any{"one", 2}}
		v, e := ht.YQ(data, ".string")
		if assert.NoError(tt, e) {
			assert.IsType(tt, "string_value", v)
			assert.Equal(tt, "string_value", v)
		}

		v, e = ht.YQ(data, ".list")
		if assert.NoError(tt, e) {
			x := []any{"one", 2}
			assert.IsType(tt, x, v)
			assert.Equal(tt, x, v)
		}
		v, e = ht.YQ(data, ".bool")
		if assert.NoError(tt, e) {
			x := true
			assert.IsType(tt, x, v)
			assert.Equal(tt, x, v)
		}
		v, e = ht.YQ(data, ".int")
		if assert.NoError(tt, e) {
			x := 16
			assert.IsType(tt, x, v)
			assert.Equal(tt, x, v)
		}
		v, e = ht.YQ(data, ".nonexistant")
		if assert.NoError(tt, e) {
			assert.Nil(tt, v)
		}
	})
}
