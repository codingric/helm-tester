package helm_tester

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

type H map[string]any

func assertNoPanic(t *testing.T, f func()) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Test failed with panic: %v", r)
			t.FailNow()
		}
	}()
	f()
}

func TestNewHelmTesterSkipUpdate(t *testing.T) {
	assertNoPanic(t, func() {
		os.Remove("./helm/charts/echo-server-0.5.0.tgz")
		NewHelmTester("./helm", WithSkipDependencyUpdate())
		_, err := os.Stat("./helm/charts/echo-server-0.5.0.tgz")
		assert.Error(t, err, "Expected no charts to be downloaded")
	})
}

func TestNewHelmTesterUpdate(t *testing.T) {
	assertNoPanic(t, func() {
		os.Remove("./helm/charts/echo-server-0.5.0.tgz")
		NewHelmTester("./helm")
		_, err := os.Stat("./helm/charts/echo-server-0.5.0.tgz")
		assert.NoError(t, err, "Expected charts to be downloaded")
	})
}

// func TestNewHelmTesterReal(t *testing.T) {
// 	h, _ := NewHelmTester("/Users/admzwh5/src/kyverno-controller/helm")
// 	h.Query(".Manifests")
// }

func TestRender(t *testing.T) {
	ht, _ := NewHelmTester("./helm", WithSkipDependencyUpdate())

	var output any
	var err error

	t.Run("render", func(tt *testing.T) {
		output, err = ht.Render(nil)
		assert.NoError(tt, err)
		if !assert.NoError(tt, err) {
			return
		}
		var result string
		err = ht.YQ(`.[]|select(.kind == "Deployment" and .metadata.name =="-echo-server")|.spec.template.spec.containers[0].image`, &result, output)
		assert.NoError(tt, err)
		if !assert.NoError(tt, err) {
			return
		}
		assert.Equal(tt, "ealen/echo-server:updated", result)
	})
	t.Run("render with values", func(tt *testing.T) {
		tag := "overriden"
		vals := map[string]interface{}{
			"echo-server": map[string]interface{}{
				"image": map[string]interface{}{
					"tag": tag,
				},
			},
		}
		output, err = ht.Render(vals)
		if !assert.NoError(tt, err) {
			return
		}
		var result string
		err = ht.YQ(`.[]|select(.kind == "Deployment" and .metadata.name =="-echo-server")|.spec.template.spec.containers[0].image`, &result, output)
		assert.NoError(tt, err)
		if !assert.NoError(tt, err) {
			return
		}
		assert.Equal(tt, "ealen/echo-server:"+tag, result)
	})
}

func TestQuery(t *testing.T) {
	ht, _ := NewHelmTester("./helm")

	t.Run("values", func(tt *testing.T) {
		var keys []string
		err := ht.YQ(`.Chart.Values|keys`, &keys)
		if !assert.NoError(tt, err) {
			return
		}
		assert.Contains(tt, keys, "echo-server")
	})
	t.Run("dependency-values", func(tt *testing.T) {
		var v string
		e := ht.YQ(`.Dependencies[1].Values.image.tag`, &v)
		if assert.NoError(tt, e) {
			assert.Equal(tt, "0.6.0", v)
		}
	})
	t.Run("manifests", func(tt *testing.T) {
		var m []any
		e := ht.YQ(`[.Manifests[].kind]`, &m)
		if assert.NoError(tt, e) {
			assert.Len(tt, m, 5)
		}

	})
	t.Run("yq.blank", func(tt *testing.T) {
		var v string
		e := ht.YQ(`.Dependencies[1].Values.image.tagxx`, &v)
		if assert.NoError(tt, e) {
			assert.Equal(tt, "", v)
		}
	})

	t.Run("yq.string", func(tt *testing.T) {
		data := `
string: "string_value"
int: 16
bool: true
list:
- one
- 2`
		var v string
		e := ht.YQ(".string", &v, data)
		if assert.NoError(tt, e) {
			assert.IsType(tt, "string_value", v)
			assert.Equal(tt, "string_value", v)
		}

		var l []any
		e = ht.YQ(".list", &l, data)
		if assert.NoError(tt, e) {
			x := []any{"one", 2}
			assert.IsType(tt, x, l)
			assert.Equal(tt, x, l)
		}

		var b bool
		e = ht.YQ(".bool", &b, data)
		if assert.NoError(tt, e) {
			x := true
			assert.IsType(tt, x, b)
			assert.Equal(tt, x, b)
		}

		var i int
		e = ht.YQ(".int", &i, data)
		if assert.NoError(tt, e) {
			x := 16
			assert.IsType(tt, x, i)
			assert.Equal(tt, x, i)
		}

		var z any
		e = ht.YQ(".nonexistant", &z, data)
		if assert.NoError(tt, e) {
			assert.Nil(tt, z)
		}
	})

	t.Run("yq.any", func(tt *testing.T) {
		data := map[string]any{"string": "string_value", "bool": true, "int": 16, "list": []any{"one", 2}}
		var v string
		e := ht.YQ(".string", &v, data)
		if assert.NoError(tt, e) {
			assert.IsType(tt, "string_value", v)
			assert.Equal(tt, "string_value", v)
		}

		var l []any
		e = ht.YQ(".list", &l, data)
		if assert.NoError(tt, e) {
			x := []any{"one", 2}
			assert.IsType(tt, x, l)
			assert.Equal(tt, x, l)
		}

		var b bool
		e = ht.YQ(".bool", &b, data)
		if assert.NoError(tt, e) {
			x := true
			assert.IsType(tt, x, b)
			assert.Equal(tt, x, b)
		}

		var i int
		e = ht.YQ(".int", &i, data)
		if assert.NoError(tt, e) {
			x := 16
			assert.IsType(tt, x, i)
			assert.Equal(tt, x, i)
		}

		var z any
		e = ht.YQ(".nonexistant", &z, data)
		if assert.NoError(tt, e) {
			assert.Nil(tt, z)
		}
	})
}
