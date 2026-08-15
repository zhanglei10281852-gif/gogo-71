# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

清除任务把留作储备的燃料也排掉了。一台 chaser：delta_v_budget_mps=329.72164、capture_slots=2，mission.reserve_fraction 配成 0.2，所以 mission 自己输出的 reserve_mps=65.94433、usable_mps=263.77731；场景里只有一个可捕获目标，那条 leg 的 leg_delta_v_mps=314.02061（altitude 6.35316 + plane 147.9193 + phasing 4 + capture 2 + disposal 153.74815）。这条 leg 明显超出可用预算，mission 还是把它排成 status=planned：planned_delta_v_mps=314.02061、remaining_mps=-50.24330、budget_ok=false，MSN-02-delta-v-budget 当场给出 fail，说 planned 超过 usable budget；timeline 最后一条 mission-end 也照样写成用 314.02061 m/s 中的 263.77731 m/s usable。把同一台 chaser 的预算压到比 leg 成本还低（例如 300 m/s）时能正常拒，写成 leg 成本远小于 usable 时也正常，只有成本落在 usable_mps 与 delta_v_budget_mps 之间这一段会被放进计划。储备是留给交会误差和撤离的，照这种计划出场，现场一点余量都没有，而且 remaining_mps 负数、budget_ok=false 还得下游自己去发现。请修复 leg 的预算判定，同时保持 reserve_mps/usable_mps 的算法、被拒 leg 的 budget-exceeded 状态与 reason 文本、capture_slots 上限、nearest-neighbour 选序、timeline 事件与 disposal 选择等既有行为不变，并保证全量测试通过。

## 含 Bug 版本

- 仓库：zhanglei10281852-gif/gogo-71
- 仓库地址：https://github.com/zhanglei10281852-gif/gogo-71.git
- parent SHA：03d71a13929caa6e8a49082957757acdfa32b710

## 复现步骤

```bash
git clone -- https://github.com/zhanglei10281852-gif/gogo-71.git bug-repro
cd bug-repro
git checkout --detach 03d71a13929caa6e8a49082957757acdfa32b710
go test ./internal/mission -run "^TestPlannedDeltaVStaysOutOfTheChaserReserve$" -count=1 -v
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/mission -run "^TestPlannedDeltaVStaysOutOfTheChaserReserve$" -count=1 -v
=== RUN   TestPlannedDeltaVStaysOutOfTheChaserReserve
    reserve_budget_regression_test.go:74: a 314.02061 m/s leg must not be flown against a 263.77731 m/s usable budget (329.72164 m/s total, 65.94433 m/s reserve); planned 314.02061 m/s
--- FAIL: TestPlannedDeltaVStaysOutOfTheChaserReserve (0.00s)
FAIL
FAIL	DebrisLedger/internal/mission	0.002s
FAIL

```

stderr：

```text
warning: internal/mission/reserve_budget_regression_test.go has type 100755, expected 100644
warning: internal/mission/reserve_budget_regression_test.go has type 100755, expected 100644

```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/mission -run "^TestPlannedDeltaVStaysOutOfTheChaserReserve$" -count=1 -v
=== RUN   TestPlannedDeltaVStaysOutOfTheChaserReserve
    reserve_budget_regression_test.go:74: a 314.02061 m/s leg must not be flown against a 263.77731 m/s usable budget (329.72164 m/s total, 65.94433 m/s reserve); planned 314.02061 m/s
--- FAIL: TestPlannedDeltaVStaysOutOfTheChaserReserve (0.02s)
FAIL
FAIL	DebrisLedger/internal/mission	0.131s
FAIL

```

stderr：

```text
warning: internal/mission/reserve_budget_regression_test.go has type 100755, expected 100644
warning: internal/mission/reserve_budget_regression_test.go has type 100755, expected 100644

```

## 通过条件

同一场景下先用宽松预算量出那条 leg 的 leg_delta_v_mps，再把 chaser 预算设成该成本的 1.05 倍、reserve_fraction=0.2（于是 usable_mps 约为成本的 0.84 倍）时：legs_planned=0，唯一的 leg 以 budget-exceeded 报出并带 reason，planned_delta_v_mps=0 且不超过 usable_mps，remaining_mps 不为负，budget_ok=true，MSN-02-delta-v-budget 为 pass；reserve_mps 与 usable_mps 仍按 reserve_fraction 计算且 usable_mps 小于 delta_v_budget_mps；预算充足时该 leg 仍照原样排入、capture_slots 上限、skipped_targets 的排序与理由、residual risk、disposal 动作与 timeline 顺序等既有行为不回归；定向测试、全量 go test ./... -count=1 与 go build ./... && go vet ./... 全部通过；校准与远端复跑均在 golang:1.22 linux/amd64 单架构完成。
