import http.server
import json

class MockLLMHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        # Suppress logging to stdout to keep it clean
        pass

    def do_POST(self):
        if self.path == '/v1/messages':
            content_length = int(self.headers['Content-Length'])
            post_data = self.rfile.read(content_length)
            
            # Formulate response containing two findings in calc.py
            findings = [
                {
                    "severity": "严重",
                    "category": "浮点数比较缺陷-直接等值比较",
                    "file_path": "calc.py",
                    "line_number": "5-8",
                    "code_snippet": "    discount = price * 0.9\n    if discount == 1.05:",
                    "title": "直接对计算后的浮点数进行等值比较 (==)",
                    "detail": "变量 discount 是通过 price * 0.9 算术运算计算得出的。直接与字面量 1.05 进行 == 比较，会因为二进制浮点精度舍入误差（例如实际上是 1.0500000000000003）而导致判定恒为 False，引发严重计费优惠跳过的业务逻辑缺陷。",
                    "suggestion": "方案一：使用 math.isclose 进行容差比较。\n\n正确代码示例：\nimport math\nif math.isclose(discount, 1.05, rel_tol=1e-9):\n    print(\"Special discount category A\")\n\n方案二：改用 decimal.Decimal 进行十进制精确计算与等值比较。"
                },
                {
                    "severity": "严重",
                    "category": "浮点数比较缺陷-直接不等比较",
                    "file_path": "calc.py",
                    "line_number": "9-10",
                    "code_snippet": "    elif discount != 2.5:",
                    "title": "直接对计算后的浮点数进行不等比较 (!=)",
                    "detail": "变量 discount 与字面量 2.5 进行 != 比较，会因为微小的浮点数精度误差而导致不等式条件意外成立，绕过特定的业务状态机校验。",
                    "suggestion": "方案一：使用 math.isclose 进行等值比较，然后取反。\n\n正确代码示例：\nimport math\nif not math.isclose(discount, 2.5, rel_tol=1e-9):\n    print(\"Normal processing\")"
                },
                {
                    "severity": "主要",
                    "category": "浮点数比较缺陷-含有等号的边界比较",
                    "file_path": "calc.py",
                    "line_number": "12-17",
                    "code_snippet": "    if value <= limit:\n        print(\"Value is within limit\")\n    elif value >= 2.5:\n        print(\"Value exceeds upper bound\")",
                    "title": "直接对计算后的浮点数进行含有等号的边界比较 (<=, >=)",
                    "detail": "变量 value 与 limit (1.05) 进行 <= 比较，或者与 2.5 进行 >= 比较。二进制浮点数舍入误差可能会导致原本应该等于边界的值微小超标（例如 1.0500000000000003），从而导致 <= 判定意外返回 False，导致边界逻辑被错误跳过。",
                    "suggestion": "方案一：使用边界偏移偏移值或结合 math.isclose 进行判断。\n\n正确代码示例：\nimport math\nif value < limit or math.isclose(value, limit, rel_tol=1e-9):\n    print(\"Value is within limit\")"
                }
            ]

            summary_text = "本次扫描重点对 Python 代码中的浮点数运算与等值/不等值/边界值比较逻辑进行了审计。共发现3处风险缺陷，分别涉及 ==、!= 以及 <=、>= 的直接判定。已给出 math.isclose 的安全替代修复建议，能有效防止计算精度丢失导致的业务运行故障。"
            
            payload = {
                "findings": findings,
                "summary": summary_text
            }
            
            response_data = {
                "id": "msg_mock_123456",
                "type": "message",
                "role": "assistant",
                "content": [
                    {
                        "type": "text",
                        "text": json.dumps(payload, ensure_ascii=False)
                    }
                ],
                "model": "gemini-3-flash-preview",
                "stop_reason": "end_turn",
                "stop_sequence": None,
                "usage": {
                    "input_tokens": 100,
                    "output_tokens": 200
                }
            }
            
            self.send_response(200)
            self.send_header('Content-Type', 'application/json; charset=utf-8')
            self.end_headers()
            self.wfile.write(json.dumps(response_data, ensure_ascii=False).encode('utf-8'))
        else:
            self.send_response(404)
            self.end_headers()

def run(port=8000):
    server_address = ('', port)
    httpd = http.server.HTTPServer(server_address, MockLLMHandler)
    print(f"Starting mock LLM server on port {port}...")
    httpd.serve_forever()

if __name__ == '__main__':
    run()
