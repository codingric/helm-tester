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
		ht.AssertQueryTrue(tt, `.Manifests|keys|contains(["test-chart/charts/echo-server/templates/deployment.yaml"])`, "Missing manifests")
	})

	t.Run("yq", func(tt *testing.T) {
		data := ""
		v, e := ht.YQ(data, `.Dependencies[1].Values.image.tag`)
		if assert.NoError(tt, e) {
			assert.Equal(tt, "0.6.0", v)
		}
		data = `name: "Ricardo"`
		v, e = ht.YQ(data, ".name")
		if assert.NoError(tt, e) {
			assert.Equal(tt, "Ricardo", v)
		}
	})
}
