package xiaohongshu

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// 由现有 go test 入口执行页面脚本用例，不额外修改工作流权限。
func TestDirectMessagePageScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") == "true" {
			t.Fatal("CI 需要 Node.js 18+ 执行私信页面脚本测试")
		}
		t.Skip("本地未安装 Node.js 18+；跳过页面脚本测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "direct_message.test.cjs")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("页面脚本测试失败: %v\n%s", err, out)
	}
	t.Logf("页面脚本测试:\n%s", out)
}
