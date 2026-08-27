package repository

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/moecard/server/internal/model"
)

// 白名单漏列是这个项目已经踩过一次的坑：ProductRepo.Update 的 Select 里
// 少了 custom_fields，接口照样返回 200、前端照样弹"保存成功"，只有那一列
// 永远写不进去。三个用结构体更新的仓储都是同一个写法，同一个坑。
//
// 这里不抄一份白名单来比，而是直接从源码里把 Select(...) 的实参解析出来，
// 再和模型的列对齐。这样两个方向的漂移都能拦住：
//   - 模型加了新列，忘了加进 Select → 保存静默失效
//   - Select 里删了一列，测试里的副本没跟着改 → 假绿
func TestUpdateWhitelistsMatchModels(t *testing.T) {
	cases := []struct {
		file    string // 源文件
		recv    string // 方法接收者类型
		model   any    // 对应的模型
		skipCol map[string]string
	}{
		{
			file: "product.go", recv: "ProductRepo", model: model.Product{},
			skipCol: map[string]string{
				"id":          "主键",
				"created_at":  "创建时间不可改",
				"deleted_at":  "软删走 SoftDelete",
				"sales_count": "销量由下单流程累加，不能被后台表单覆盖",
			},
		},
		{
			file: "product.go", recv: "CategoryRepo", model: model.Category{},
			skipCol: map[string]string{
				"id": "主键", "created_at": "创建时间不可改", "deleted_at": "软删走 SoftDelete",
			},
		},
		{
			file: "coupon.go", recv: "CouponRepo", model: model.Coupon{},
			skipCol: map[string]string{
				"id": "主键", "created_at": "创建时间不可改", "deleted_at": "软删走 SoftDelete",
				"used_count": "核销次数由用券流程累加，不能被后台表单覆盖",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.recv, func(t *testing.T) {
			got := selectArgsOf(t, c.file, c.recv, "Update")
			if len(got) == 0 {
				t.Fatalf("没在 %s 的 %s.Update 里找到 Select(...)；"+
					"改成不带白名单的 Updates 会把计数器一起覆盖掉", c.file, c.recv)
			}

			white := map[string]bool{}
			for _, col := range got {
				white[col] = true
			}
			if !white["updated_at"] {
				t.Errorf("%s.Update 的白名单里没有 updated_at，改动时间不会刷新", c.recv)
			}

			var missing, unknown []string
			cols := columnsOf(c.model)
			for _, col := range cols {
				if !white[col] && c.skipCol[col] == "" {
					missing = append(missing, col)
				}
			}
			for col := range white {
				if col == "updated_at" {
					continue
				}
				if !contains(cols, col) {
					unknown = append(unknown, col)
				}
			}

			if len(missing) > 0 {
				t.Errorf("这些列既不在 %s.Update 的白名单里，也没在测试里声明为不可编辑：%v\n"+
					"加可编辑列时要同时改仓储，否则保存会静默失败。", c.recv, missing)
			}
			if len(unknown) > 0 {
				t.Errorf("%s.Update 的白名单里有模型上不存在的列：%v\n"+
					"列改名后白名单没跟着改，那一列同样写不进去。", c.recv, unknown)
			}
		})
	}
}

// selectArgsOf 从源码里取出某个方法体内 Select(...) 的字符串实参。
//
// 只认字面量：白名单本来就该是一眼能读完的常量，拼出来的名字既不好审也不好查。
func selectArgsOf(t *testing.T, file, recv, method string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", file, err)
	}

	var out []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != method || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		if id, ok := star.X.(*ast.Ident); !ok || id.Name != recv {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Select" {
				return true
			}
			for _, a := range call.Args {
				lit, ok := a.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				// 有的写法把多列塞进一个字符串，逗号分开
				for _, part := range strings.Split(s, ",") {
					if p := strings.TrimSpace(part); p != "" {
						out = append(out, p)
					}
				}
			}
			return true
		})
	}
	return out
}

// columnsOf 取模型上所有真正落库的列名（含内嵌 Model）。
func columnsOf(m any) []string {
	var cols []string
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				walk(f.Type)
				continue
			}
			tag := f.Tag.Get("gorm")
			if tag == "-" || !strings.Contains(tag, "column:") {
				continue
			}
			cols = append(cols, strings.SplitN(strings.SplitN(tag, "column:", 2)[1], ";", 2)[0])
		}
	}
	walk(reflect.TypeOf(m))
	return cols
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
