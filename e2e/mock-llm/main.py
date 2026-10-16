import json
import base64
import struct
import zlib


def png_bytes(width=256, height=256):
    row = b"\x00" + bytes([46, 102, 84]) * width
    raw = row * height
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xffffffff)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")
from http.server import BaseHTTPRequestHandler, HTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/healthz":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"status":"ok"}')
            return
        self.send_error(404)

    def do_POST(self):
        if self.path == "/v1/images/generations":
            image = base64.b64encode(png_bytes()).decode()
            response = json.dumps({"created": 1, "data": [{"b64_json": image}]}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(response)))
            self.end_headers()
            self.wfile.write(response)
            return
        if self.path != "/v1/chat/completions":
            self.send_error(404)
            return
        length = int(self.headers.get("Content-Length", "0"))
        body = json.loads(self.rfile.read(length)) if length else {}
        system = body.get("messages", [{}])[0].get("content", "")
        if "创意总监" in system:
            content = json.dumps({"proposals": [
                {"title": "山岚玉青", "concept": "以山岚和玉青色建立自然东方识别", "design_strategy": {"direction": "自然东方"}, "color_palette": ["#2e6654", "#dce9df"], "typography": "现代宋体", "difference_summary": "自然、克制、适合年轻茶饮", "image_prompt": "elegant tea gift box, jade green and mountain mist, modern oriental packaging"},
                {"title": "墨金山水", "concept": "用墨色与金色表现高端礼赠", "design_strategy": {"direction": "高端礼赠"}, "color_palette": ["#1d2523", "#c58d42"], "typography": "高端人文宋体", "difference_summary": "稳重、高端、适合节日礼赠", "image_prompt": "premium black and gold tea gift box, ink landscape, luxury packaging mockup"},
                {"title": "现代茶席", "concept": "以留白和几何图形呈现轻量化茶生活", "design_strategy": {"direction": "现代生活"}, "color_palette": ["#f5f1e8", "#789b8b"], "typography": "现代无衬线", "difference_summary": "年轻、轻盈、适合日常消费", "image_prompt": "minimal modern tea gift box, warm white and sage green, geometric packaging"}
            ]}, ensure_ascii=False)
        elif "融合总监" in system:
            content = json.dumps({"title": "山岚玉青 × 墨金山水融合版", "concept": "保留自然东方叙事与墨金高端识别，以留白控制视觉密度", "design_strategy": {"direction": "平衡东方与现代高端", "source_balance": "自然东方 + 高端礼赠"}, "color_palette": ["#2e6654", "#c58d42", "#f5f1e8"], "typography": "现代人文宋体", "difference_summary": "综合来源方案的文化叙事、金色工艺和留白构图，降低装饰密度以提升货架识别", "image_prompt": "balanced modern oriental tea gift box, jade green black and gold, restrained luxury packaging mockup"}, ensure_ascii=False)
        elif "迭代 Agent" in system:
            content = json.dumps({"title": "山岚玉青 · 升级版", "concept": "保留山岚玉青的自然主色，减少插画复杂度并突出低糖卖点", "design_strategy": {"direction": "自然东方升级", "revision": "突出低糖卖点"}, "color_palette": ["#2e6654", "#dce9df"], "typography": "现代宋体", "difference_summary": "根据客户反馈简化插画并强化卖点", "image_prompt": "revised jade green tea gift box, simplified mountain illustration, clear low sugar label, packaging mockup"}, ensure_ascii=False)
        else:
            content = json.dumps({
                "brief_patch": {"product_category": "茶饮", "product_name": "E2E测试红茶", "audience": "年轻消费者", "package_type": "礼盒", "required_text": ["产品名称", "净含量"], "material": "哑光纸盒", "package_spec": {"width_mm": 120, "height_mm": 180, "depth_mm": 60}},
                "assistant_message": "已记录包装需求，Brief 已达到生成方案所需完整度。", "next_questions": [], "completion_score": 100
            }, ensure_ascii=False)
        response = json.dumps({
            "id": "mock-completion",
            "object": "chat.completion",
            "model": "mock-intake",
            "choices": [{"index": 0, "message": {"role": "assistant", "content": content}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 50, "completion_tokens": 100, "total_tokens": 150},
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)

    def log_message(self, *_):
        pass


HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
