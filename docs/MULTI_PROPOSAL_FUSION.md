# 多方案融合设计

## 数据结构

`design_fusions` 是项目级融合会话，保存来源方案、来源特征快照、客户确认的要素决策、权重、约束、融合 Brief 和输出方案 ID。`design_proposals.fusion_id` 将生成方案反向关联到融合会话，保证每个取舍都能追溯。

核心字段：

- `source_proposal_ids`：2–5 个同项目来源方案。
- `feature_selections`：`dimension`、`value`、`source_proposal_id`、`decision`、`weight`。
- `weights`：策略、视觉、生产等维度的权重。
- `constraints`：品牌、成本、结构和工艺限制。
- `status`：`draft`、`queued`、`completed`。

## 页面流程

方案列表下方提供“多方案融合”卡片：

1. 勾选 2–5 个来源方案。
2. 填写策略、视觉语言、生产约束和额外限制。
3. 保存融合规则并提交异步生成任务。
4. 查看任务状态和融合历史。
5. 生成方案回到方案列表，继续评分、反馈、修订和导出。

## API 流程

```text
GET  /api/v1/projects/{project_id}/fusions
POST /api/v1/projects/{project_id}/fusions
GET  /api/v1/projects/{project_id}/fusions/{fusion_id}
PATCH /api/v1/projects/{project_id}/fusions/{fusion_id}
POST /api/v1/projects/{project_id}/fusions/{fusion_id}/generate
GET  /api/v1/tasks/{task_id}
```

生成任务由 Redis/Asynq 调度，融合 Agent 接收 Brief、来源方案摘要和客户取舍规则，输出一套新方案及图片资产。生成完成后更新融合会话的 `output_proposal_ids`，并推进项目轮次，原有反馈和导出 API 可以继续使用。
