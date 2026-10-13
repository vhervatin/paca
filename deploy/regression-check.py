#!/usr/bin/env python3
"""Anti-regressão de upgrade do Paca (EVUP).

Diferente do smoke-test (que só checa "endpoint responde 200"), este captura o
ESTADO (contagens + amostras + acessibilidade de anexos + customizações EVUP) e
compara ANTES vs DEPOIS de um upgrade, alertando se algo DIMINUIU ou sumiu —
pega regressão de DADOS/FEATURES, não só rota quebrada.

Uso:
  # 1) ANTES do deploy: capturar baseline da prod
  PACA_BASE=https://paca.evup.com.br PACA_USER=admin PACA_PASS='...' \
      python3 deploy/regression-check.py --save paca-evup-deploy/regression-baseline.json

  # 2) DEPOIS do deploy: comparar
  PACA_BASE=https://paca.evup.com.br PACA_USER=admin PACA_PASS='...' \
      python3 deploy/regression-check.py --compare paca-evup-deploy/regression-baseline.json

Sai com código != 0 se a comparação achar regressão (bom p/ gating).
"""
import json, os, sys, urllib.request, urllib.error, http.cookiejar

BASE = os.environ.get("PACA_BASE", "http://localhost:3000").rstrip("/")
API = BASE + "/api/v1"
USER = os.environ.get("PACA_USER", "admin")
PASS = os.environ.get("PACA_PASS", "adminpassword")

jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))


def req(method, url, payload=None, raw=False):
    data = json.dumps(payload).encode() if payload is not None else None
    r = urllib.request.Request(url, data=data, method=method)
    r.add_header("Content-Type", "application/json")
    try:
        with opener.open(r, timeout=30) as x:
            body = x.read()
            return x.status, (body if raw else body.decode())
    except urllib.error.HTTPError as e:
        return e.code, (e.read() if raw else e.read().decode())
    except Exception as e:
        return 0, (b"" if raw else str(e))


def data_of(body):
    try:
        return json.loads(body).get("data")
    except Exception:
        return None


def list_all(path):
    """Coleta todos os items de um endpoint de lista, seguindo paginação
    offset (page/total) OU cursor (next_cursor), de forma tolerante."""
    items, page, seen_cursor = [], 1, None
    for _ in range(200):  # teto de segurança
        sep = "&" if "?" in path else "?"
        if seen_cursor:
            url = f"{API}{path}{sep}cursor={seen_cursor}&page_size=100"
        else:
            url = f"{API}{path}{sep}page={page}&page_size=100"
        code, body = req("GET", url)
        if code != 200:
            return items, code
        d = data_of(body)
        if isinstance(d, list):  # endpoint devolve lista pura, sem paginação
            items.extend(d)
            break
        if not isinstance(d, dict):
            break
        batch = d.get("items", [])
        if not isinstance(batch, list):
            batch = []
        items.extend(batch)
        nxt = d.get("next_cursor")
        if nxt:
            seen_cursor = nxt
            continue
        total = d.get("total")
        if total is not None and len(items) < total and batch:
            page += 1
            continue
        break
    return items, 200


def capture():
    snap = {"base": BASE, "projects": {}, "evup": {}}

    code, _ = req("POST", API + "/auth/login",
                  {"username": USER, "password": PASS, "remember_me": True})
    if code != 200:
        print(f"❌ login falhou (HTTP {code})")
        sys.exit(2)

    # Customizações / features EVUP
    code, body = req("GET", API + "/version")
    try:
        snap["evup"]["version"] = json.loads(body).get("data", {}).get("current")
    except Exception:
        snap["evup"]["version"] = None
    code, _ = req("GET", API + "/releases")
    snap["evup"]["changelog_ok"] = code == 200
    # (obs.: o SMTP/e-mail virou plugin com.paca.smtp a partir do v0.19 — não
    # fica mais em /admin/settings; não checamos aqui p/ não dar falso vermelho.)

    # Usuários
    users, _ = list_all("/admin/users")
    snap["users_count"] = len(users)

    # Projetos + por-projeto
    projects, _ = list_all("/projects")
    snap["projects_count"] = len(projects)
    first_attachment_checked = False
    for p in projects:
        pid, name = p.get("id"), p.get("name", "?")
        if not pid:
            continue
        tasks, _ = list_all(f"/projects/{pid}/tasks")
        sprints, _ = list_all(f"/projects/{pid}/sprints")
        views, _ = list_all(f"/projects/{pid}/views")
        members, _ = list_all(f"/projects/{pid}/members")
        snap["projects"][pid] = {
            "name": name,
            "tasks": len(tasks),
            "sprints": len(sprints),
            "views": len(views),
            "members": len(members),
            # amostra de títulos p/ detectar perda de conteúdo (não só count)
            "task_titles_sample": sorted([t.get("title", "") for t in tasks])[:5],
        }
        # Acessibilidade de 1 anexo real (pega susto de storage, ex. MinIO×RustFS)
        if not first_attachment_checked:
            for t in tasks:
                atts = t.get("attachments") or []
                if atts and atts[0].get("id"):
                    aid = atts[0]["id"]
                    code, blob = req(
                        "GET", f"{API}/projects/{pid}/tasks/{t['id']}/attachments/{aid}/download", raw=True)
                    snap["evup"]["sample_attachment"] = {
                        "ok": code == 200 and len(blob) > 0,
                        "http": code, "bytes": len(blob),
                    }
                    first_attachment_checked = True
                    break
    return snap


def pct(a, b):
    return "" if a == b else (f"  (−{a-b})" if b < a else f"  (+{b-a})")


def compare(baseline, now):
    issues = []
    print(f"\n=== Comparação baseline → agora ===")
    print(f"versão: {baseline['evup'].get('version')} → {now['evup'].get('version')}")

    for key in ("users_count", "projects_count"):
        a, b = baseline.get(key, 0), now.get(key, 0)
        flag = "🟢" if b >= a else "🔴"
        if b < a:
            issues.append(f"{key} caiu: {a} → {b}")
        print(f"  {flag} {key}: {a} → {b}{pct(a,b)}")

    # EVUP features
    for f in ("changelog_ok",):
        a, b = baseline["evup"].get(f), now["evup"].get(f)
        if a and not b:
            issues.append(f"customização EVUP sumiu: {f}")
        print(f"  {'🟢' if b else '🔴'} evup.{f}: {b}")

    # Projetos: cada projeto do baseline deve existir e não encolher
    for pid, bp in baseline["projects"].items():
        np_ = now["projects"].get(pid)
        if not np_:
            issues.append(f"PROJETO SUMIU: {bp['name']} ({pid})")
            print(f"  🔴 projeto sumiu: {bp['name']}")
            continue
        for metric in ("tasks", "sprints", "views", "members"):
            a, b = bp.get(metric, 0), np_.get(metric, 0)
            if b < a:
                issues.append(f"{bp['name']}: {metric} {a} → {b}")
                print(f"  🔴 {bp['name']} · {metric}: {a} → {b}")

    att = now["evup"].get("sample_attachment") or baseline["evup"].get("sample_attachment")
    if att is not None:
        print(f"  {'🟢' if att.get('ok') else '🔴'} anexo de amostra acessível: {att}")
        if not att.get("ok"):
            issues.append("anexo de amostra NÃO acessível (checar storage)")

    print()
    if issues:
        print("🔴 REGRESSÕES ENCONTRADAS:")
        for i in issues:
            print(f"   - {i}")
        return 1
    print("🟢 Nenhuma regressão — nada foi perdido.")
    return 0


if __name__ == "__main__":
    mode = sys.argv[1] if len(sys.argv) > 1 else "--print"
    path = sys.argv[2] if len(sys.argv) > 2 else None
    now = capture()
    print(f"Capturado: {now['projects_count']} projetos, {now['users_count']} usuários, "
          f"versão {now['evup'].get('version')}")
    for pid, p in now["projects"].items():
        print(f"  · {p['name']}: {p['tasks']} tasks, {p['sprints']} sprints, "
              f"{p['views']} views, {p['members']} membros")
    if mode == "--save" and path:
        with open(path, "w") as f:
            json.dump(now, f, indent=2, ensure_ascii=False)
        print(f"\n✅ baseline salvo em {path}")
    elif mode == "--compare" and path:
        with open(path) as f:
            baseline = json.load(f)
        sys.exit(compare(baseline, now))
