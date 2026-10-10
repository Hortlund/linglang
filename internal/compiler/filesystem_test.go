package compiler

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDeveloperFilesystem(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	os.MkdirAll(filepath.Join(target, "nested"), 0700)
	link := filepath.Join(dir, "alias")
	if err := os.Symlink("target/nested", link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(target, "file.lang")
	original := "preserve me"
	os.WriteFile(file, []byte(original), 0750)
	explicit := filepath.Join(dir, "source-link")
	os.Symlink("target/file.lang", explicit)
	os.Symlink("cycle", filepath.Join(dir, "cycle"))
	canonical, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`func main(){
 root:=%q
 assert(pathInfo(root+"/alias").kind=="symlink")
 assert(canonicalPath(root+"/alias/../file.lang").value==%q)
 assert(canonicalPath(root+"/source-link").value==%q)
 assert(!canonicalPath(root+"/cycle").ok)
 assert(!canonicalPath(root+"/absent").ok)
 assert(!replaceFile(root+"/source-link","preserve me","bad").ok)
 assert(!replaceFile(%q,"outdated","bad").ok)
 assert(readFile(%q).value=="preserve me")
 assert(replaceFile(%q,"preserve me","new bytes").ok)
 assert(!writeNewFile(%q,"overwrite").ok)
 assert(!writeNewFile(root+"/source-link","overwrite").ok)
 assert(makeDirectories(root+"/new/deep").ok)
 assert(pathInfo(root+"/new/deep").kind=="directory")
 assert(writeNewFile(root+"/new/deep/one.lang","one").ok)
 assert(head(readDirectory(root+"/new/deep").value).value=="one.lang")
 assert(sha256("")=="e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
 assert(sha256("abc")=="ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
 assert(sha256("\x00\xff雪")==%q)
 assert(!writeNewFile(root+"/new/deep","bad").ok)
}`, dir, canonical, canonical, file, file, file, file, fmt.Sprintf("%x", sha256.Sum256([]byte("\x00\xff雪"))))
	for _, baseline := range []bool{false, true} {
		os.WriteFile(file, []byte(original), 0750)
		os.RemoveAll(filepath.Join(dir, "new"))
		out, err := executeScriptWithOptions(t, source, `linglang_rt:set_gc_stress(true),linglang_program:main(),halt(0).`, Options{DisableOptimizations: baseline})
		if err != nil || out != "" {
			t.Fatalf("filesystem baseline=%v: %v %s", baseline, err, out)
		}
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != 0750 {
			t.Fatalf("mode lost: %v", err)
		}
		data, _ := os.ReadFile(file)
		if string(data) != "new bytes" {
			t.Fatal("wrong replacement")
		}
	}
}
