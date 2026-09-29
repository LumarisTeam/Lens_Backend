#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""bug-feedback 后端端到端联调脚本（无需前端即可验证全链路）。

依赖: 仅 Python 标准库（Python 3.9+）。
前置: 后端已启动，环境变量（含 ADMIN_API_TOKEN）来自 Password.env。
用法: python scripts/e2e_smoke.py [base_url] [client_id]

流程: healthz -> 创建反馈中心 -> presign -> (负例: gif 拒绝) -> PUT 直传 COS -> confirm
      -> (负例: 未知 file_key) -> (负例: Magic Number 校验)
      -> (负例: 缺少校验头) -> submit -> 重放拦截 -> 幂等重放
      -> admin 列表 -> admin 详情 -> 图片 URL 可下载。
"""
import json
import os
import struct
import sys
import time
import urllib.error
import urllib.request
import uuid
import zlib

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8080"
CLIENT_ID = sys.argv[2] if len(sys.argv) > 2 else "e2e-smoke-" + uuid.uuid4().hex[:12]
TOKEN = os.environ.get("ADMIN_API_TOKEN", "")
REQUEST_ID = "e2e-smoke-" + uuid.uuid4().hex
E2E_SN = "E2E-" + uuid.uuid4().hex[:12].upper()

PASS = 0
FAIL = 0
RESULTS = []


def check(name, ok, detail=""):
    global PASS, FAIL
    tag = "PASS" if ok else "FAIL"
    if ok:
        PASS += 1
    else:
        FAIL += 1
    RESULTS.append((tag, name, detail))
    print(f"[{tag}] {name}" + (f"  ({detail})" if detail and not ok else ""))


def req(method, url, body=None, headers=None, timeout=30):
    """发送请求，返回 (status, parsed_json_or_None, raw_text)。"""
    h = dict(headers or {})
    data = None
    if body is not None:
        data = body if isinstance(body, bytes) else json.dumps(body).encode("utf-8")
        if not isinstance(body, bytes):
            h.setdefault("Content-Type", "application/json")
    r = urllib.request.Request(url, data=data, headers=h, method=method)
    try:
        with urllib.request.urlopen(r, timeout=timeout) as resp:
            raw = resp.read()
            return resp.status, _try_json(raw), raw
    except urllib.error.HTTPError as e:
        raw = e.read()
        return e.code, _try_json(raw), raw
    except Exception as e:  # 网络层异常
        return 0, None, f"{type(e).__name__}: {e}"


def _try_json(raw):
    try:
        return json.loads(raw)
    except Exception:
        return None


def make_png(width=3, height=2, rgb=(214, 51, 132)):
    """纯标准库生成合法 PNG（zlib + struct + crc32）。"""
    def chunk(tag, data):
        return (struct.pack(">I", len(data)) + tag + data +
                struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))
    sig = b"\x89PNG\r\n\x1a\n"
    ihdr = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)  # 8bit RGB
    raw = b"".join(b"\x00" + bytes(rgb) * width for _ in range(height))
    return sig + chunk(b"IHDR", ihdr) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")


def sleep_gentle():
    time.sleep(0.4)  # RPS=5 / burst=10，留余量


def admin_headers():
    return {
        "Authorization": "Bearer " + TOKEN,
        "X-Operator": "e2e-smoke",
    }


def generate_code(center_id):
    body = {
        "timestamp": int(time.time()),
        "sn": E2E_SN,
        "nonce": uuid.uuid4().hex,
        "ttl": 300,
    }
    st, j, _ = req(
        "POST",
        BASE + f"/api/admin/feedback-centers/{center_id}/codes/generate",
        body,
        admin_headers(),
    )
    data = (j or {}).get("data") or {}
    ok = st == 200 and j and j.get("code") == 0 and data.get("code")
    return ok, st, data


def main():
    print(f"base={BASE} client_id={CLIENT_ID} request_id={REQUEST_ID}")
    if not TOKEN:
        print("[WARN] ADMIN_API_TOKEN 为空，admin 步骤将失败（先加载 Password.env）")
    if any(ord(c) > 255 for c in TOKEN):
        print("[WARN] ADMIN_API_TOKEN 含非 Latin-1 字符（疑似占位符未替换），HTTP 头无法发送")

    # 1. 健康检查
    st, j, _ = req("GET", BASE + "/healthz")
    check("healthz", st == 200 and j and j.get("code") == 0, f"status={st}")
    sleep_gentle()

    # 2. 创建反馈中心并生成第一枚校验码
    expire_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() + 86400))
    st, j, _ = req(
        "POST",
        BASE + "/api/admin/feedback-centers",
        {
            "name": "E2E Smoke Center",
            "appId": "e2e-smoke",
            "env": "test",
            "snMode": "whitelist",
            "snList": [E2E_SN],
            "expireAt": expire_at,
            "contact": "e2e@example.com",
            "remark": "automated smoke test",
        },
        admin_headers(),
    )
    center_id = (j or {}).get("data", {}).get("centerId")
    ok = st == 200 and j and j.get("code") == 0 and center_id
    check("创建反馈中心", ok, f"status={st}")
    if not ok:
        print(json.dumps(j, ensure_ascii=False))
        return 1
    sleep_gentle()

    ok_code, st, code_data = generate_code(center_id)
    check("生成校验码", ok_code, f"status={st}")
    if not ok_code:
        print(json.dumps(code_data, ensure_ascii=False))
        return 1
    feedback_headers = {
        "X-Feedback-Center-Id": center_id,
        "X-Timestamp": str(code_data["timestamp"]),
        "X-SN": E2E_SN,
        "X-Nonce": code_data["nonce"],
        "X-Code": code_data["code"],
    }
    sleep_gentle()

    # 3. presign（生成合法 PNG）
    png = make_png()
    st, j, _ = req("POST", BASE + "/api/v1/uploads/presign",
                   {"filename": "e2e-test.png", "size": len(png), "mime_type": "image/png"},
                   {"X-Client-ID": CLIENT_ID})
    ok = st == 200 and j and j.get("code") == 0 and j.get("data", {}).get("upload_url")
    check("presign(png)", ok, f"status={st}")
    if not ok:
        print(json.dumps(j, ensure_ascii=False))
        return 1
    file_key = j["data"]["file_key"]
    upload_url = j["data"]["upload_url"]
    sleep_gentle()

    # 4. 负例：gif 不在白名单
    st, j, _ = req("POST", BASE + "/api/v1/uploads/presign",
                   {"filename": "x.gif", "size": 10, "mime_type": "image/gif"},
                   {"X-Client-ID": CLIENT_ID})
    check("presign(gif) 应 415", st == 415 and j and j.get("code") == 41501, f"status={st}")
    sleep_gentle()

    # 5. PUT 直传 COS（Content-Length 必须精确）
    st, j, _ = req("PUT", upload_url, png, {"Content-Type": "image/png"})
    check("PUT 直传 COS", st == 200, f"status={st}")
    sleep_gentle()

    # 6. confirm
    st, j, _ = req("POST", BASE + "/api/v1/uploads/confirm",
                   {"file_key": file_key}, {"X-Client-ID": CLIENT_ID})
    ok = st == 200 and j and j.get("code") == 0
    check("confirm", ok, f"status={st}")
    attachment_id = (j or {}).get("data", {}).get("attachment_id")
    sleep_gentle()

    # 7. 负例：未知 file_key
    st, j, _ = req("POST", BASE + "/api/v1/uploads/confirm",
                   {"file_key": "feedback/2099/01/01/DOESNOTEXIST.png"},
                   {"X-Client-ID": CLIENT_ID})
    check("confirm(未知key) 应 404", st == 404 and j and j.get("code") == 40401, f"status={st}")
    sleep_gentle()

    # 8. 负例：声明 jpeg 实传 png（Magic Number 校验）
    st, j, _ = req("POST", BASE + "/api/v1/uploads/presign",
                   {"filename": "fake.jpg", "size": len(png), "mime_type": "image/jpeg"},
                   {"X-Client-ID": CLIENT_ID})
    if st == 200 and j and j.get("code") == 0:
        fake_url = j["data"]["upload_url"]
        fake_key = j["data"]["file_key"]
        req("PUT", fake_url, png, {"Content-Type": "image/jpeg"})
        sleep_gentle()
        st, j, _ = req("POST", BASE + "/api/v1/uploads/confirm",
                       {"file_key": fake_key}, {"X-Client-ID": CLIENT_ID})
        check("MagicNumber 校验应 415", st == 415 and j and j.get("code") == 41501, f"status={st}")
    else:
        check("MagicNumber 负例 presign", False, f"presign 意外失败 status={st}")
    sleep_gentle()

    # 9. 负例：缺少反馈中心校验头
    st, j, _ = req(
        "POST",
        BASE + "/api/v1/feedbacks",
        {
            "request_id": REQUEST_ID,
            "content": "missing code",
            "contact": "13800000000",
            "attachment_ids": [],
            "extra": {"app_name": "e2e-smoke"},
        },
        {"X-Client-ID": CLIENT_ID},
    )
    check("缺少校验头应 40001", st == 400 and j and j.get("code") == 40001, f"status={st}")
    sleep_gentle()

    # 10. submit
    st, j, _ = req("POST", BASE + "/api/v1/feedbacks",
                   {"request_id": REQUEST_ID,
                    "content": "【E2E联调测试】本机自动验证，可删除。",
                    "contact": "13800000000",
                    "attachment_ids": [attachment_id] if attachment_id else [],
                    "extra": {"app_name": "e2e-smoke", "note": "auto-test"}},
                   {"X-Client-ID": CLIENT_ID, **feedback_headers})
    ok = st == 200 and j and j.get("code") == 0 and j.get("data", {}).get("feedback_no")
    check("submit 反馈", ok, f"status={st}")
    feedback_no = (j or {}).get("data", {}).get("feedback_no")
    sleep_gentle()

    # 11. 同一 code 重放必须被 Redis 防重放拦截
    st, j, _ = req(
        "POST",
        BASE + "/api/v1/feedbacks",
        {
            "request_id": REQUEST_ID,
            "content": "replay",
            "contact": "13800000000",
            "attachment_ids": [],
            "extra": {"app_name": "e2e-smoke"},
        },
        {"X-Client-ID": CLIENT_ID, **feedback_headers},
    )
    check("相同校验码重放应 40006", st == 409 and j and j.get("code") == 40006, f"status={st}")
    sleep_gentle()

    # 12. 幂等：换新校验码、同 request_id 重放，应返回同一 feedback_no
    ok_code, st, code_data = generate_code(center_id)
    if not ok_code:
        check("幂等重放前生成校验码", False, f"status={st}")
    idem_headers = {
        "X-Feedback-Center-Id": center_id,
        "X-Timestamp": str(code_data.get("timestamp", "")),
        "X-SN": E2E_SN,
        "X-Nonce": code_data.get("nonce", ""),
        "X-Code": code_data.get("code", ""),
    }
    st2, j2, _ = req("POST", BASE + "/api/v1/feedbacks",
                     {"request_id": REQUEST_ID, "content": "重放", "contact": "13800000000",
                      "attachment_ids": [], "extra": {"app_name": "e2e-smoke"}},
                     {"X-Client-ID": CLIENT_ID, **idem_headers})
    same = st2 == 200 and j2 and j2.get("data", {}).get("feedback_no") == feedback_no
    check("幂等重放(同client)", same, f"status={st2}")
    sleep_gentle()

    # 13. admin 列表
    st, j, _ = req("GET", BASE + "/api/v1/admin/feedbacks?app_name=e2e-smoke&page=1&page_size=20",
                   None, {"Authorization": "Bearer " + TOKEN})
    total = (j or {}).get("data", {}).get("total")
    check("admin 列表", st == 200 and total is not None and total >= 1, f"status={st} total={total}")
    sleep_gentle()

    # 14. admin 详情 + 图片 URL
    if feedback_no:
        st, j, _ = req("GET", BASE + f"/api/v1/admin/feedbacks/{feedback_no}",
                       None, {"Authorization": "Bearer " + TOKEN})
        images = (j or {}).get("data", {}).get("images", [])
        check("admin 详情", st == 200 and len(images) >= 1 and images[0].get("url"), f"status={st} images={len(images)}")
        if images and images[0].get("url"):
            st, j, raw = req("GET", images[0]["url"])
            is_png = st == 200 and raw[:8] == b"\x89PNG\r\n\x1a\n"
            check("图片 URL 可下载且为 PNG", is_png, f"status={st}")
    else:
        check("admin 详情", False, "无 feedback_no，跳过")

    print("\n===== 结果: %d 通过, %d 失败 =====" % (PASS, FAIL))
    print("清理测试数据（可选，在数据库中执行）:")
    print(f"  DELETE FROM feedback WHERE request_id = '{REQUEST_ID}';")
    print(f"  DELETE FROM feedback_center WHERE center_id = '{center_id}';")
    print("  -- 附件行会自动脱钩（feedback_id SET NULL），COS 对象 24h 后由孤儿清理删除")
    return 0 if FAIL == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
