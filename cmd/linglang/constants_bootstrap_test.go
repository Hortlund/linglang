package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"go/constant"
	"go/types"
	"math/big"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"linglang/internal/compiler"
)

func TestBootstrapConstantsAgainstSeed(t *testing.T) {
	expressions := []string{
		"-9223372036854775808", "9223372036854775807", "1<<511", "(1<<500)/(1<<200)",
		"((1<<200)+17)-((1<<199)*2)", "((1<<200)+17)%((1<<100)+7)",
		"-(1<<200)/7", "-(1<<200)%7", "-7/3", "-7%3", "-7>>2", "-1>>1074", "0<<1074",
		"((1<<400)+3)>>399", "^((1<<300)+7)", "-(1<<200)&((1<<199)+17)",
		"-(1<<200)|17", "-(1<<200)^(-((1<<199)+7))", "-(1<<200)&^(-((1<<199)+7))",
		"((1<<200)+17) == ((1<<200)+18)", "(1<<200) > (1<<199)", "!(true && false)",
		`"a\x00\xff"+"雪"`, "`raw\r\n雪`", `"\a\b\f\n\r\t\v\\\"\000\377\u96ea\U0001f600"`,
		`"ab\ncd\\ef\u96eaGH"`, `""`, "``", `"plain 雪"`, "`\rstart\r\nend\r`",
		"'雪'", `'\xff'`, `'\U0001f600'`, "len(\"雪\\x00\\xff\")", "len(string(0x1f600))",
		"string(0xd800)", "string(-1)", "string(1<<100)", "int('雪')", `"\x61" == "a"`,
	}
	for _, operator := range []string{"+", "-", "*", "/", "%", "&", "|", "^", "&^", "<", "=="} {
		for _, signs := range []string{"", "-"} {
			expressions = append(expressions, fmt.Sprintf("%s((1<<73)+123) %s (-((1<<39)+7))", signs, operator))
		}
	}
	var sources []string
	for _, expression := range expressions {
		sources = append(sources, "package main\nconst Value="+expression)
	}
	sources = append(sources,
		"package main\nconst(Value=Other+iota;Other=iota)",
		"package main\nconst(A=iota;B;Value=A+iota)",
		"package main\nconst(A int=iota+1;Value)",
	)
	base := "package main\nconst S0=\"ab\";"
	for i := 1; i <= 13; i++ {
		base += fmt.Sprintf("const S%d=S%d+S%d;", i, i-1, i-1)
	}
	for _, expression := range []string{"len(S13)", "S13", "S13==S12+S12", "(S13+\"a\")<(S13+\"b\")", "(S13+\"a\")>S13", "S13!=S12", "S13 < \"c\"", "S13 > \"aa\""} {
		sources = append(sources, base+"const Value="+expression)
	}
	var literals []string
	var want strings.Builder
	for i, source := range sources {
		analysis := compiler.Analyze([]compiler.SourceFile{{Filename: "fixture.lang", Source: []byte(source)}})
		if len(analysis.Diagnostics) != 0 {
			t.Fatalf("seed rejected %s: %+v", source, analysis.Diagnostics)
		}
		var value constant.Value
		for id, object := range analysis.Info.Defs {
			if id.Name == "Value" {
				if c, ok := object.(*types.Const); ok {
					value = c.Val()
				}
			}
		}
		if value == nil {
			t.Fatal("missing seed constant")
		}
		identity := ""
		switch value.Kind() {
		case constant.Int:
			n, ok := new(big.Int).SetString(value.ExactString(), 10)
			if !ok {
				t.Fatal(value)
			}
			identity = "i:"
			if n.Sign() < 0 {
				identity += "-"
				n.Abs(n)
			}
			mask := big.NewInt((1 << 30) - 1)
			for n.Sign() != 0 {
				identity += new(big.Int).And(n, mask).String() + ":"
				n.Rsh(n, 30)
			}
		case constant.String:
			identity = "s:" + hex.EncodeToString([]byte(constant.StringVal(value)))
		case constant.Bool:
			identity = "b:" + strconv.FormatBool(constant.BoolVal(value))
		default:
			t.Fatal(value)
		}
		fmt.Fprintf(&want, "%d %s\n", i, identity)
		literals = append(literals, strconv.Quote(source))
	}
	driver := `package main
func valueHex(text string)string{
 digits:="0123456789abcdef";var reversed List[string]
 for i:=0;i<len(text);i++{b:=byteAt(text,i);reversed=prepend(slice(digits,b/16,b/16+1)+slice(digits,b%16,b%16+1),reversed)}
 var parts List[string];for _,part:=range reversed{parts=prepend(part,parts)};return join(parts,"")
}
func main(){
 fixtures:=List[string]{` + strings.Join(literals, ",") + `}
 for i,source:=range fixtures{
  parsed:=parse("fixture.lang",source);assert(parsed.ok)
  result:=checkPackage(List[SyntaxNode]{parsed.root});if !result.ok{panic(result.reason)}
  found:=get(result.constants,"Value");assert(found.ok);value:=found.value
  if value.kind=="bool"{if value.boolean{println(i,"b:true")}else{println(i,"b:false")}}else if value.kind=="string"{println(i,"s:"+valueHex(constantStringBytes(value)))}else{println(i,constantIdentity(value))}
 }
}`
	files, err := readSources("../../bootstrap/checker")
	if err != nil {
		t.Fatal(err)
	}
	var library []compiler.SourceFile
	for _, file := range files {
		if filepath.Base(file.Filename) != "main.lang" {
			library = append(library, file)
		}
	}
	library = append(library, compiler.SourceFile{Filename: "constant_oracle.lang", Source: []byte(driver)})
	for _, baseline := range []bool{false, true} {
		t.Run(fmt.Sprintf("baseline=%v", baseline), func(t *testing.T) {
			program, err := compiler.CompileFilesWithOptions(library, compiler.Options{DisableOptimizations: baseline})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := build(dir, program); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, "erl", "-noshell", "-pa", dir, "-eval", evaluationScript(false, true)).CombinedOutput()
			if err != nil || !strings.HasPrefix(string(out), want.String()) {
				t.Fatalf("constant oracle mismatch: %v\ngot: %q\nwant prefix: %q", err, out, want.String())
			}
			if !strings.Contains(string(out), "live_cells => 0") || !strings.Contains(string(out), "root_frames => 0") {
				t.Fatalf("leaked roots: %s", out)
			}
		})
	}
}
