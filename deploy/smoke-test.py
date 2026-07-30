#!/usr/bin/env python3
"""Smoke test pós-deploy do Paca (EVUP).

Valida as rotas-chave que as páginas principais dependem no carregamento —
pega regressões como a página de agentes quebrando por 500 em /agents/llm-models.
Sai com código != 0 se qualquer verificação falhar (bom pra CI / pós-deploy).

Uso:
  # produção (login admin)
  PACA_BASE=https://paca.evup.com.br PACA_USER=admin PACA_PASS='...' python3 deploy/smoke-test.py
  # dev (chave do agente)
  PACA_BASE=http://localhost:3000 PACA_API_KEY=dev-agent-api-key-change-in-production python3 deploy/smoke-test.py
"""
import json, os, sys, urllib.request, urllib.error, http.cookiejar

BASE = os.environ.get("PACA_BASE", "http://localhost:3000").rstrip("/")
API = BASE + "/api/v1"
USER = os.environ.get("PACA_USER", "admin")
PASS = os.environ.get("PACA_PASS", "adminpassword")
API_KEY = os.environ.get("PACA_API_KEY")  # se setado, usa X-API-Key em vez de login

jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

def req(method, url, payload=None, auth=True):
    data = json.dumps(payload).encode() if payload is not None else None
    r = urllib.request.Request(url, data=data, method=method)
    r.add_header("Content-Type", "application/json")
    if auth and API_KEY:
        r.add_header("X-API-Key", API_KEY)
    try:
        with opener.open(r, timeout=25) as x:
            return x.status, x.read().decode()
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()
    except Exception as e:
        return 0, str(e)

results = []
def check(name, cond, detail=""):
    results.append((name, cond, detail))
    print(f"  {'✅' if cond else '❌'} {name}" + (f"  — {detail}" if detail and not cond else ""))

print(f"Smoke test → {BASE}\n")

# 1) infra pública
code, body = req("GET", BASE + "/api/healthz", auth=False)
check("healthz 200", code == 200, f"HTTP {code}")

code, body = req("GET", API + "/version", auth=False)
ok = code == 200 and '"current"' in body
check("version 200 + current", ok, f"HTTP {code}")

code, body = req("GET", API + "/releases", auth=False)
ok = code == 200 and '"releases"' in body
check("releases 200 (changelog)", ok, f"HTTP {code}")

# 2) SPA carrega
code, body = req("GET", BASE + "/", auth=False)
ok = code == 200 and "<title>Paca</title>" in body
check("SPA / 200 + <title>", ok, f"HTTP {code}")

# 3) auth (login, se não usar API key)
if not API_KEY:
    code, body = req("POST", API + "/auth/login", {"username": USER, "password": PASS, "remember_me": True}, auth=False)
    check("login admin 200", code == 200, f"HTTP {code}")

# 4) rotas autenticadas que as páginas carregam
code, body = req("GET", API + "/projects")
ok = code == 200 and '"items"' in body
check("projects 200", ok, f"HTTP {code}")

# o bug real: página de agentes depende disto — tem que ser 200, nunca 500
code, body = req("GET", API + "/agents/llm-models")
check("agents/llm-models 200 (nunca 500)", code == 200, f"HTTP {code} {body[:120]}")

# global-roles (a página quebrou uma vez); o endpoint tem que responder
code, body = req("GET", API + "/admin/global-roles")
check("admin/global-roles 200", code == 200, f"HTTP {code}")

# member picker (bug: "nenhum usuário disponível") — a listagem que alimenta o
# dialog "Adicionar membro" tem que responder 200 com itens. A causa raiz era o
# front pedir page_size acima do cap do backend (100) → 400 → lista vazia.
if not API_KEY:
    code, body = req("GET", API + "/admin/users?page=1&page_size=20")
    ok = code == 200 and '"items"' in body
    check("admin/users (picker de membro) 200 + items", ok, f"HTTP {code}")
    # guarda o cap: pedir acima de 100 tem que ser rejeitado (não pode voltar a passar)
    code, _ = req("GET", API + "/admin/users?page=1&page_size=500")
    check("admin/users respeita cap de page_size (400 acima de 100)", code == 400, f"HTTP {code}")

# roles de um projeto (agents page carrega)
pid = None
_, pbody = req("GET", API + "/projects")
try:
    items = json.loads(pbody)["data"]["items"]
    pid = items[0]["id"] if items else None
except Exception:
    pass
if pid:
    code, body = req("GET", f"{API}/projects/{pid}/roles")
    check("projects/{id}/roles 200", code == 200, f"HTTP {code}")
    code, body = req("GET", f"{API}/projects/{pid}/agents")
    check("projects/{id}/agents 200", code == 200, f"HTTP {code}")

# resumo
passed = sum(1 for _, c, _ in results if c)
total = len(results)
print(f"\n=== {passed}/{total} OK ===")
sys.exit(0 if passed == total else 1)
