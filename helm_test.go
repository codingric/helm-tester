package helm_tester

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type H map[string]any

func TestRender(t *testing.T) {
	ht := NewHelmTester("./helm")

	var output any
	var err error

	t.Run("render", func(tt *testing.T) {
		output, err = ht.Render(nil)
		assert.NoError(tt, err)
		if !assert.NoError(tt, err) {
			return
		}
		var result string
		err = ht.YQ(output, `.[]|select(.kind == "Deployment" and .metadata.name =="-echo-server")|.spec.template.spec.containers[0].image`, &result)
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
		err = ht.YQ(output, `.[]|select(.kind == "Deployment" and .metadata.name =="-echo-server")|.spec.template.spec.containers[0].image`, &result)
		assert.NoError(tt, err)
		if !assert.NoError(tt, err) {
			return
		}
		assert.Equal(tt, "ealen/echo-server:"+tag, result)
	})
}

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
		var v string
		e := ht.YQ(nil, `.Dependencies[1].Values.image.tag`, &v)
		if assert.NoError(tt, e) {
			assert.Equal(tt, "0.6.0", v)
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
		e := ht.YQ(data, ".string", &v)
		if assert.NoError(tt, e) {
			assert.IsType(tt, "string_value", v)
			assert.Equal(tt, "string_value", v)
		}

		var l []any
		e = ht.YQ(data, ".list", &l)
		if assert.NoError(tt, e) {
			x := []any{"one", 2}
			assert.IsType(tt, x, l)
			assert.Equal(tt, x, l)
		}

		var b bool
		e = ht.YQ(data, ".bool", &b)
		if assert.NoError(tt, e) {
			x := true
			assert.IsType(tt, x, b)
			assert.Equal(tt, x, b)
		}

		var i int
		e = ht.YQ(data, ".int", &i)
		if assert.NoError(tt, e) {
			x := 16
			assert.IsType(tt, x, i)
			assert.Equal(tt, x, i)
		}

		var z any
		e = ht.YQ(data, ".nonexistant", &z)
		if assert.NoError(tt, e) {
			assert.Nil(tt, z)
		}
	})

	t.Run("yq.any", func(tt *testing.T) {
		data := map[string]any{"string": "string_value", "bool": true, "int": 16, "list": []any{"one", 2}}
		var v string
		e := ht.YQ(data, ".string", &v)
		if assert.NoError(tt, e) {
			assert.IsType(tt, "string_value", v)
			assert.Equal(tt, "string_value", v)
		}

		var l []any
		e = ht.YQ(data, ".list", &l)
		if assert.NoError(tt, e) {
			x := []any{"one", 2}
			assert.IsType(tt, x, l)
			assert.Equal(tt, x, l)
		}

		var b bool
		e = ht.YQ(data, ".bool", &b)
		if assert.NoError(tt, e) {
			x := true
			assert.IsType(tt, x, b)
			assert.Equal(tt, x, b)
		}

		var i int
		e = ht.YQ(data, ".int", &i)
		if assert.NoError(tt, e) {
			x := 16
			assert.IsType(tt, x, i)
			assert.Equal(tt, x, i)
		}

		var z any
		e = ht.YQ(data, ".nonexistant", &z)
		if assert.NoError(tt, e) {
			assert.Nil(tt, z)
		}
	})
}
