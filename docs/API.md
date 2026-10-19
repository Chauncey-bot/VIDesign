# Packaging AI API v1

基础地址：`/api/v1`。所有响应均为 JSON。耗时任务返回 `202 Accepted` 和 `task_id`，任务由 Redis + Asynq 持久化调度。

## 用户注册和登录

`POST /auth/register`

```json
{"email":"customer@example.com","password":"至少8位密码"}
```

`POST /auth/login` 使用相同请求格式。响应返回 `token`，后续 API 请求携带：

```text
Authorization: Bearer <token>
```

项目和方案数据按登录用户隔离。

## 项目列表

`GET /projects`

返回当前客户的历史项目，按最近更新时间倒序排列。创建项目时不再由客户端指定 `customer_id`，服务端从登录 Token 中确定项目归属。

## 创建项目

`POST /projects`

请求：

```json
{"name":"松萝茶包装升级","customer_id":"customer-id"}
```

响应 `201`：返回 `Project` 对象。

## 获取项目

`GET /projects/{project_id}`

返回项目状态、当前轮次和 Brief 摘要。

## 对话采集需求

`POST /projects/{project_id}/chat`

支持 `application/json` 或 `multipart/form-data`。上传 Word 时使用字段 `document`，仅支持 `.docx`，正文和内嵌图片会被提取并作为需求上下文。

```json
{"message":"我们希望做一套适合年轻人的红茶礼盒"}
```

Multipart 字段：

```text
message: 客户补充说明
document: brief.docx
```

响应 `202`。消息会进入异步任务队列；Worker 执行需求采集 Agent 后，将 Brief 新版本写入 MySQL，并把项目状态推进到 `collecting_requirements` 或 `brief_ready`。

```json
{"task_id":"task-id","status":"queued"}
```

## 查询异步任务

`GET /tasks/{task_id}`

返回 `queued`、`running`、`succeeded` 或 `failed`。任务成功后，可重新请求项目 Brief。

示例：

```json
{"task_id":"task-id","status":"queued"}
```

## 查看或更新 Brief

`GET /projects/{project_id}/brief`

`PATCH /projects/{project_id}/brief`

PATCH 示例：

```json
{
  "audience":"25-40岁城市女性",
  "material":"哑光纸盒",
  "required_text":["产品名称","净含量","生产商信息"]
}
```

## 生成第一轮方案

`POST /projects/{project_id}/generate`

```json
{"proposal_count":9,"product_name":"松萝茶·山岚玉青","reference_asset_ids":[]}
```

`product_name` 可选。填写后，所有方案都必须围绕该产品名称进行包装文字层级、识别性和视觉设计；留空时，系统会为每套方案生成不同的 `product_name`。

响应 `202`：返回任务 ID。任务完成后按 `proposal_count` 生成对应数量的 `DesignProposal`（工作台默认 9 套，每次由 Agent 分批生成 3 套）。

## 查看方案

`GET /projects/{project_id}/proposals?round=1`

返回方案列表、产品名称、设计策略、配色、字体和图片资产。每个方案包含可选的 `product_name` 字段。

## 提交客户反馈

`POST /proposals/{proposal_id}/feedback`

```json
{
  "rating":4,
  "comment":"喜欢颜色，但插画太复杂，希望突出低糖卖点",
  "liked_items":["颜色"],
  "disliked_items":["插画复杂"],
  "must_change":["突出低糖卖点"],
  "optional_change":[]
}
```

## 多方案融合

融合流程把多个已生成方案保存为一个可追溯的融合会话。会话记录来源方案、客户确认的保留/调整/排除项、权重、约束和输出方案。

查看项目融合记录：

```text
GET /projects/{project_id}/fusions
```

创建融合会话：

```json
{
  "name": "东方文化 × 现代高端平衡版",
  "source_proposal_ids": ["proposal-a", "proposal-b"],
  "source_features": {"proposals": [{"id":"proposal-a","title":"宫廷朱砂"}]},
  "feature_selections": [
    {"dimension":"strategy","value":"保留方案 A 的文化叙事","source_proposal_id":"proposal-a","decision":"keep","weight":90},
    {"dimension":"visual","value":"采用方案 B 的深色金色调","source_proposal_id":"proposal-b","decision":"adapt","weight":80}
  ],
  "weights": {"strategy":0.4,"visual":0.35,"production":0.25},
  "constraints": ["控制专色数量", "适配天地盖盒结构"]
}
```

```text
POST /projects/{project_id}/fusions
GET  /projects/{project_id}/fusions/{fusion_id}
PATCH /projects/{project_id}/fusions/{fusion_id}
POST /projects/{project_id}/fusions/{fusion_id}/generate
```

`generate` 返回异步 `task_id`。任务完成后，融合方案会出现在 `GET /projects/{project_id}/proposals` 中，并带有 `fusion_id`；融合记录的 `output_proposal_ids` 保存来源与输出的关系。

## 启动二轮设计

`POST /projects/{project_id}/revision`

```json
{
  "selected_proposal_id":"proposal-id",
  "feedback_ids":["feedback-id"],
  "preserve":["主色","品牌字体"]
}
```

响应 `202`：返回二轮任务 ID。

## 审核和导出

`POST /projects/{project_id}/approve`

```json
{"proposal_id":"proposal-id"}
```

`POST /projects/{project_id}/export`

```json
{"formats":["png","pdf"],"include_design_brief":true}
```

导出响应会返回下载地址：

```json
{
  "project_id":"project-id",
  "downloads":[
    {"format":"pdf","url":"/api/v1/projects/project-id/export?format=pdf"},
    {"format":"markdown","url":"/api/v1/projects/project-id/export?format=markdown"}
  ]
}
```

下载 PDF 或设计说明：

```text
GET /projects/{project_id}/export?format=pdf
GET /projects/{project_id}/export?format=markdown
GET /projects/{project_id}/export?format=zip

`format=zip` 返回一个打包文件，包含中文 PDF 报告、设计说明和全部方案图片。
```

下载单个方案图片：

```text
GET /assets/{asset_id}
```

## 错误格式

```json
{"error":{"code":"brief_incomplete","message":"请先补充包装尺寸"}}
```
