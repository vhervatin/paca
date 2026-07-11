# Deploy do fork EVUP no EasyPanel

Guia para publicar este fork do Paca numa VPS com [EasyPanel](https://easypanel.io).
A ideia: o GitHub builda as imagens (GHCR), o EasyPanel só as baixa e roda o
stack via **Compose**, com o Caddy do Paca como gateway atrás do proxy do
EasyPanel (que cuida do domínio + HTTPS).

> Substitua `paca.SEU-DOMINIO.com.br` pelo seu domínio real e `vhervatin`
> pelo dono do seu fork, se for diferente.

---

## Pré-requisitos

- VPS com **EasyPanel** instalado.
- Um **domínio** (ou subdomínio) com o registro DNS **A** apontando para o IP da VPS.
- Este fork no GitHub com o workflow `.github/workflows/evup-release.yml` presente.

---

## Etapa 1 — Publicar as imagens (GitHub → GHCR)

As imagens são construídas pelo GitHub Actions, não pela VPS.

1. Na branch `evup`, crie e envie uma tag de versão:
   ```bash
   git tag v0.9.4-evup.1
   git push origin v0.9.4-evup.1
   ```
   Isso dispara o workflow **evup-release**, que publica:
   - `ghcr.io/vhervatin/paca-api:0.9.4-evup.1`
   - `ghcr.io/vhervatin/paca-web:0.9.4-evup.1`
   - `ghcr.io/vhervatin/paca-realtime:0.9.4-evup.1`

2. Acompanhe em **GitHub → Actions**. Ao terminar, torne cada pacote **público**
   (uma vez só): GitHub → seu perfil → **Packages** → `paca-api` → *Package
   settings* → *Change visibility* → **Public**. Repita para `paca-web` e
   `paca-realtime`. (Público = o EasyPanel puxa sem senha. Se preferir privado,
   configure uma credencial do GHCR no EasyPanel.)

---

## Etapa 2 — Gerar os segredos

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

## Etapa 3 — Criar o serviço no EasyPanel

1. **Crie um projeto** (ex.: `paca`).
2. Adicione um serviço **Compose** (*Services → Compose (Beta)*).
3. Em **Content**, cole o conteúdo de [`deploy/docker-compose.prod.yml`](./docker-compose.prod.yml)
   deste repositório (ele já sobe postgres, valkey, minio, api, web, realtime e
   o gateway Caddy).
4. Em **Environment** (o `.env` do compose), preencha:
   ```env
   # Imagens do fork (GHCR)
   PACA_API_IMAGE=ghcr.io/vhervatin/paca-api:0.9.4-evup.1
   PACA_WEB_IMAGE=ghcr.io/vhervatin/paca-web:0.9.4-evup.1
   PACA_REALTIME_IMAGE=ghcr.io/vhervatin/paca-realtime:0.9.4-evup.1

   # URL pública e cookies (HTTPS via EasyPanel)
   PUBLIC_URL=https://paca.SEU-DOMINIO.com.br
   COOKIE_SECURE=true
   CORS_ORIGINS=https://paca.SEU-DOMINIO.com.br

   # Segredos (da Etapa 2)
   POSTGRES_PASSWORD=...
   JWT_SECRET=...
   ENCRYPTION_KEY=...
   ADMIN_USERNAME=admin
   ADMIN_PASSWORD=...
   STORAGE_ACCESS_KEY_ID=...
   STORAGE_SECRET_ACCESS_KEY=...
   STORAGE_PUBLIC_URL=https://paca.SEU-DOMINIO.com.br/storage

   # Aviso de nova versão na home
   PACA_VERSION=v0.9.4-evup.1
   RELEASE_UPSTREAM_REPO=Paca-AI/paca
   RELEASE_FORK_REPO=vhervatin/paca

   # Sem AI agent por enquanto (ver Notas)
   ```
5. **Domínio + HTTPS**: aponte o domínio para o serviço **gateway** na **porta 80**
   (é o Caddy, a porta de entrada única do Paca). O EasyPanel provisiona o
   certificado Let's Encrypt automaticamente. Deixe os demais serviços **sem**
   domínio — quem fala com o mundo é só o gateway.
6. **Deploy**. Acompanhe os logs até o `api` mostrar `starting server` e o
   `gateway` ficar de pé.

> O `db-backup` e o `ai-agent` do compose podem ser removidos/ignorados neste
> primeiro deploy (veja Notas).

---

## Etapa 4 — Primeiro acesso

Abra `https://paca.SEU-DOMINIO.com.br` e entre com `ADMIN_USERNAME` /
`ADMIN_PASSWORD`. As migrações do banco rodam sozinhas no start do `api`.

---

## Atualizações (subir uma versão nova)

1. Faça as mudanças na branch `evup`, incorpore o upstream se quiser
   (`git fetch upstream --tags && git merge upstream/master`).
2. Nova tag + push: `git tag v0.9.5-evup.1 && git push origin v0.9.5-evup.1`
   → o Actions publica as novas imagens.
3. No EasyPanel, atualize as tags `PACA_*_IMAGE` e `PACA_VERSION` no ambiente e
   **Deploy** de novo.

---

## Notas

- **AI agent**: fica de fora por padrão. Ele precisa de acesso ao **socket do
  Docker** do host para criar sandboxes, o que é sensível num painel
  compartilhado como o EasyPanel — melhor tratar à parte quando for necessário.
- **Storage**: o MinIO sobe junto (dados persistem no volume do EasyPanel).
  Para usar AWS S3, defina `STORAGE_PROVIDER=s3` + credenciais e remova o MinIO.
- **Arquitetura de CPU**: o workflow builda para `linux/amd64` (VPS x86 comum).
  Se sua VPS for ARM (Ampere/Graviton), adicione `linux/arm64` em
  `platforms:` no `.github/workflows/evup-release.yml`.
- **Plano B (sem Compose)**: se preferir serviços nativos do EasyPanel, dá para
  criar `postgres` e `redis` pelos *templates*, e `api`/`web`/`realtime` como
  *Apps* — mas aí o Caddyfile precisa ser adaptado aos hostnames internos do
  EasyPanel. O caminho via Compose acima evita isso.
