# Deploy do fork EVUP no EasyPanel

Guia para publicar este fork do Paca numa VPS com [EasyPanel](https://easypanel.io).
Fluxo: o **GitHub Actions** builda as imagens e publica no **GHCR** (privado); o
EasyPanel baixa (autenticado) e roda o stack via **Compose**, com o Caddy do
Paca como gateway atrás do proxy do EasyPanel (que cuida do domínio + HTTPS).

Repositório privado: `vhervatin/paca-evup` (branch `evup`).
Imagens: `ghcr.io/vhervatin/paca-{api,web,realtime}`.

> Substitua `paca.SEU-DOMINIO.com.br` pelo seu domínio real.

---

## Pré-requisitos

- VPS com **EasyPanel** instalado.
- Um **domínio** (ou subdomínio) gerenciado (ex.: na Cloudflare).

---

## Etapa 0 — DNS (faça PRIMEIRO)

O HTTPS do EasyPanel é emitido pelo Let's Encrypt, que valida o domínio na
hora — o registro DNS precisa existir **antes** do Passo 4 (domínio no gateway),
senão o certificado falha.

1. Descubra o **IP público da VPS** onde o EasyPanel roda.
2. Na **Cloudflare** (DNS do `evup.com.br`), crie um registro:
   - **Type:** A · **Name:** `paca` · **Content:** `<IP da VPS>`
   - **Proxy status:** **DNS only** (nuvem **cinza**) — não Proxied.
3. ⚠️ Mantenha "DNS only" **até o certificado ser emitido** no EasyPanel. O
   proxy laranja da Cloudflare intercepta o desafio do Let's Encrypt e quebra a
   emissão. Depois que o site estiver no ar por HTTPS, se quiser o proxy da
   Cloudflare (CDN/WAF/esconder IP), ligue a nuvem laranja **e** troque o
   SSL/TLS da Cloudflare para **Full (strict)**.

Os passos 1–3 abaixo não dependem do DNS; só o Passo 4 exige.

---

## Etapa 1 — Publicar as imagens (já feito na 1ª vez)

As imagens são construídas pelo GitHub Actions, não pela VPS. Para publicar uma
versão, crie e envie uma tag na branch `evup`:

```bash
git tag v0.9.4-evup.1
git push personal v0.9.4-evup.1
```

Isso dispara o workflow **evup-release** (`.github/workflows/evup-release.yml`),
que publica:
- `ghcr.io/vhervatin/paca-api:0.9.4-evup.1`
- `ghcr.io/vhervatin/paca-web:0.9.4-evup.1`
- `ghcr.io/vhervatin/paca-realtime:0.9.4-evup.1`

As imagens ficam **privadas** por padrão (ver Etapa 2 para o acesso).

---

## Etapa 2 — Credencial do GHCR (imagens privadas)

Como as imagens são privadas, o EasyPanel precisa autenticar no `ghcr.io`.

1. Gere um **Personal Access Token (classic)** no GitHub:
   Settings → Developer settings → Personal access tokens → **Tokens (classic)**
   → Generate new token → marque **apenas** o escopo **`read:packages`** → copie.
2. No EasyPanel, cadastre a credencial do registry (em *Settings → Registry* ou
   no campo de credencial da imagem, conforme a versão do painel):
   - **Registry:** `ghcr.io`
   - **Username:** `vhervatin`
   - **Password:** o token gerado acima

---

## Etapa 3 — Gerar os segredos da aplicação

Rode localmente e guarde os valores:
```bash
POSTGRES_PASSWORD=$(openssl rand -hex 32)
JWT_SECRET=$(openssl rand -hex 32)
ENCRYPTION_KEY=$(openssl rand -hex 32)     # precisa ter 64 chars hex
ADMIN_PASSWORD=$(openssl rand -hex 16)
STORAGE_ACCESS_KEY_ID=$(openssl rand -hex 12)
STORAGE_SECRET_ACCESS_KEY=$(openssl rand -hex 24)
```

---

## Etapa 4 — Criar o serviço no EasyPanel

1. **Crie um projeto** (ex.: `paca`).
2. Adicione um serviço **Compose** (*Services → Compose (Beta)*).
3. Em **Content**, cole o conteúdo de [`deploy/docker-compose.prod.yml`](./docker-compose.prod.yml)
   (já sobe postgres, valkey, minio, api, web, realtime e o gateway Caddy).
4. Em **Environment** (o `.env` do compose), preencha:
   ```env
   # Imagens privadas do fork (GHCR)
   PACA_API_IMAGE=ghcr.io/vhervatin/paca-api:0.9.4-evup.1
   PACA_WEB_IMAGE=ghcr.io/vhervatin/paca-web:0.9.4-evup.1
   PACA_REALTIME_IMAGE=ghcr.io/vhervatin/paca-realtime:0.9.4-evup.1

   # URL pública e cookies (HTTPS via EasyPanel)
   PUBLIC_URL=https://paca.SEU-DOMINIO.com.br
   COOKIE_SECURE=true
   CORS_ORIGINS=https://paca.SEU-DOMINIO.com.br
   STORAGE_PUBLIC_URL=https://paca.SEU-DOMINIO.com.br/storage

   # Segredos (da Etapa 3)
   POSTGRES_PASSWORD=...
   JWT_SECRET=...
   ENCRYPTION_KEY=...
   ADMIN_USERNAME=admin
   ADMIN_PASSWORD=...
   STORAGE_ACCESS_KEY_ID=...
   STORAGE_SECRET_ACCESS_KEY=...

   # Aviso de nova versão na home
   PACA_VERSION=v0.9.4-evup.1
   RELEASE_UPSTREAM_REPO=Paca-AI/paca
   # fork privado não é consultável pela API pública do GitHub; deixe vazio
   # (o aviso mostra só as novidades do upstream Paca-AI/paca).
   RELEASE_FORK_REPO=
   ```
5. **Domínio + HTTPS**: aponte o domínio para o serviço **gateway** na **porta 80**
   (é o Caddy, a porta de entrada única do Paca). O EasyPanel provisiona o
   certificado Let's Encrypt automaticamente. Deixe os demais serviços **sem**
   domínio — quem fala com o mundo é só o gateway.
6. **Deploy**. Acompanhe os logs até o `api` mostrar `starting server` e o
   `gateway` ficar de pé.

> `db-backup` e `ai-agent` do compose podem ser removidos/ignorados neste
> primeiro deploy (ver Notas).

---

## Etapa 5 — Primeiro acesso

Abra `https://paca.SEU-DOMINIO.com.br` e entre com `ADMIN_USERNAME` /
`ADMIN_PASSWORD`. As migrações do banco rodam sozinhas no start do `api`.

---

## Atualizações (subir uma versão nova)

1. Faça as mudanças na branch `evup` (e incorpore o upstream se quiser:
   `git fetch upstream --tags && git merge upstream/master`).
2. Nova tag + push: `git tag v0.9.5-evup.1 && git push personal v0.9.5-evup.1`
   → o Actions publica as novas imagens.
3. No EasyPanel, atualize as tags `PACA_*_IMAGE` e `PACA_VERSION` no ambiente e
   **Deploy** de novo.

---

## Notas

- **AI agent**: fica de fora por padrão. Ele precisa de acesso ao **socket do
  Docker** do host para criar sandboxes, o que é sensível num painel como o
  EasyPanel — tratar à parte quando necessário.
- **Storage**: o MinIO sobe junto (dados persistem no volume do EasyPanel).
  Para usar AWS S3, defina `STORAGE_PROVIDER=s3` + credenciais e remova o MinIO.
- **Arquitetura de CPU**: o workflow builda para `linux/amd64`. Se a VPS for ARM,
  adicione `linux/arm64` em `platforms:` no `.github/workflows/evup-release.yml`.
- **Remotes git**: `personal` = `vhervatin/paca-evup` (privado, deploy);
  `origin` = `vhervatin/paca` (fork público, PR de tradução); `upstream` =
  `Paca-AI/paca` (fonte das evoluções).
