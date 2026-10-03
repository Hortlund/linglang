package compiler

import (
	"strings"
	"testing"
)

func TestBitwiseRuntime(t *testing.T) {
	out, err := execute(t, `
func operand(order *int, n int) int { *order = *order*10+n; return n }
func main() {
 a := 13; b := 6
 assert(a & b == 4 && a | b == 15 && a ^ b == 11 && a &^ b == 9 && ^a == -14)
 a &= b; assert(a == 4); a |= 8; assert(a == 12); a ^= 3; assert(a == 15); a &^= 5; assert(a == 10)
 a <<= 3; assert(a == 80); a >>= 4; assert(a == 5)
 a = -7; b = 1; assert(a >> b == -4)
 a = 1; b = 63; assert(a << b == -9223372036854775808)
 b = 64; assert(a << b == 0 && a >> b == 0)
 a = -1; assert(a >> b == -1)
 b = 9223372036854775807; assert(a << b == 0 && a >> b == -1)
 assert(a << 18446744073709551615 == 0 && a >> 18446744073709551615 == -1)
 order := 0; assert(operand(&order, 1) << operand(&order, 2) == 4 && order == 12)
 order = 0; assert(operand(&order, 1) & operand(&order, 2) == 0 && order == 12)
 x := 12; p := &x; *p &^= 8; *p <<= 2; assert(x == 16)
 println("bitwise ok")
}`)
	if err != nil || out != "bitwise ok\n" {
		t.Fatalf("got %q (%v)", out, err)
	}
}

func TestNegativeRuntimeShifts(t *testing.T) {
	for _, operation := range []string{"<<", ">>", "<<=", ">>="} {
		t.Run(operation, func(t *testing.T) {
			statement := "println(a " + operation + " b)"
			if strings.HasSuffix(operation, "=") {
				statement = "a " + operation + " b; println(a)"
			}
			out, err := execute(t, "func main(){ a:=1; b:=-1; "+statement+" }")
			if err == nil || !strings.Contains(out, "linglang_negative_shift") {
				t.Fatalf("got %q (%v)", out, err)
			}
		})
	}
}

func TestUnsignedShiftCountContext(t *testing.T) {
	out, err := execute(t, `
func operand(order *int,n int)int{*order=*order*10+n;return n}
func main(){
 n:=0;x:=1
 assert(x<<(((1<<63)<<n)>>62)==4)
 assert(x<<(((1<<63)<<n)/(1<<62))==4)
 assert(x<<(((1<<63)<<n)%3)==4)
 assert(x<<((1<<n)+((1<<100)-(1<<100)))==2)
 assert(x<<(((1<<n)+18446744073709551615)>>63)==1)
 assert(x<<(((1<<n)+18446744073709551615)/2)==1)
 assert(x<<((1<<n)+(1<<63))==0)
 x=16;x>>=(((1<<63)<<n)>>62);assert(x==4)
 x=1;x<<=(((1<<63)<<n)/(1<<62));assert(x==4)
 p:=&x;*p<<=(((1<<63)<<n)%3);assert(x==16)
 n=1;assert(x<<(-(1<<n))==0 && x>>(-(1<<n))==0)
 x<<=-(1<<n);assert(x==0)
 order:=0;assert(operand(&order,1)<<(-(1<<operand(&order,2)))==0 && order==12)
 println("unsigned counts ok")
}`)
	if err != nil || out != "unsigned counts ok\n" {
		t.Fatalf("got %q (%v)", out, err)
	}
}

func TestUnrepresentableShiftCountOperand(t *testing.T) {
	for _, count := range []string{
		"(-1)<<n", "(1<<64)<<n",
		"(1<<n)+(1<<64)", "(1<<64)+(1<<n)",
		"(1<<n)-(1<<64)", "(1<<n)*(1<<64)",
		"(1<<n)/(1<<64)", "(1<<n)%(1<<64)",
		"(1<<n)&(1<<64)", "(1<<n)|(1<<64)",
		"^((1<<n)+(1<<64))", "(1<<n)+(-1)",
		"(1<<n)+Huge", "((1<<n)+(1<<64))>>64",
	} {
		for _, baseline := range []bool{false, true} {
			for _, use := range []string{"println(x<<(" + count + "))", "println(x>>(" + count + "))", "x<<=" + count, "x>>=" + count, "p:=&x;*p<<=" + count} {
				_, err := CompileWithOptions("test.lang", []byte("package main\nconst Huge=1<<64\nfunc main(){n:=1;x:=1;"+use+"}"), Options{DisableOptimizations: baseline})
				if err == nil || !strings.Contains(err.Error(), "overflows unsigned shift count") {
					t.Fatalf("%s, baseline %v: %v", use, baseline, err)
				}
			}
		}
	}
}

func TestRuntimeRuneContexts(t *testing.T) {
	for _, expression := range []string{
		"println('a'<<n)", "println('a'>>n)", "println(('a'<<n)==0)",
		"println(('a'<<n)<0)", "println(^('a'<<n))", "println(-('a'<<n))",
		"println(('a'<<n)+1)", "_ = 'a'<<n", "panic('a'<<n)",
		"send(self(),'a'<<n)",
	} {
		source := []byte("package main\nfunc main(){n:=30;" + expression + "}")
		t.Run(expression, func(t *testing.T) {
			analysis := Analyze([]SourceFile{{Filename: "rune.lang", Source: source}})
			if len(analysis.Diagnostics) != 1 || analysis.Diagnostics[0].Filename != "rune.lang" || analysis.Diagnostics[0].Offset < len("package main\n") || !strings.Contains(analysis.Diagnostics[0].Message, "runtime rune expressions") {
				t.Fatalf("missing source diagnostic: %+v", analysis.Diagnostics)
			}
			for _, baseline := range []bool{false, true} {
				module, err := CompileWithOptions("rune.lang", source, Options{DisableOptimizations: baseline})
				if err == nil || module != "" || !strings.Contains(err.Error(), "rune.lang:2:") || !strings.Contains(err.Error(), "runtime rune expressions") {
					t.Fatalf("baseline %v: module %q, error %v", baseline, module, err)
				}
			}
		})
	}
	for _, baseline := range []bool{false, true} {
		out, err := executeScriptWithOptions(t, `
func take(x int)int{return x}
func shifted(n int)int{return 'a'<<n}
func main(){
 n:=30;var x int='a'<<n
 assert(x==104152956928 && take('a'<<n)==x && shifted(n)==x)
 assert((('a'<<n)+len(""))==x && ('a'<<n)==x)
 n=32;x='a'<<n;assert(x==416611827712 && ('a'<<n)==x)
 x=1;n=0;assert(x<<('a'<<n)==0)
 assert('a'==97 && 'a'<<2==388)
 println("rune contexts ok")
}`, "linglang_program:main(), halt().", Options{DisableOptimizations: baseline})
		if err != nil || out != "rune contexts ok\n" {
			t.Fatalf("baseline %v: %q (%v)", baseline, out, err)
		}
	}
}
