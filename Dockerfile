# Rationale for the choices below: Docker_notes.md §8.

# Stage 1 — build. Discarded, so the toolchain and caches never ship.
FROM golang:1.26.5-trixie AS build

WORKDIR /src

# Deps before source: a code edit must not refetch modules.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Passed by `make docker-build`; .git is not in the build context.
ARG VERSION=dev

RUN CGO_ENABLED=0 go build -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/codemypaper ./cmd/codemypaper

# Stage 2 — runtime. Python because the tool executes generated Python;
# -slim not alpine because torch ships glibc-only (manylinux) wheels.
FROM python:3.12-slim

# Before the binary COPY: this layer is 868MB, the binary changes every commit.
# torch from the CPU index (PyPI's pulls multi-GB of CUDA); numpy from PyPI
# (the CPU index only mirrors numpy to 2.4.4). Versions match the host .venv.
# Cleanup is in this RUN because a later rm would not shrink the layer.
RUN pip install --no-cache-dir --root-user-action=ignore \
        --index-url https://download.pytorch.org/whl/cpu \
        torch==2.13.0+cpu \
 && pip install --no-cache-dir --root-user-action=ignore \
        numpy==2.5.1 \
 && rm -rf /root/.cache /tmp/* /var/tmp/*

RUN useradd --uid 1000 --user-group --no-create-home runner \
 && mkdir -p /work/out \
 && chown -R runner:runner /work

# HOME=/tmp so a `--user <uid>` override still has somewhere writable.
ENV HOME=/tmp \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

# After the pip layer, or site-packages would not be byte-compiled.
ENV PYTHONDONTWRITEBYTECODE=1

COPY --from=build /out/codemypaper /usr/local/bin/codemypaper

# /work because main.go defaults --out to a CWD-relative out/<id>.
WORKDIR /work
USER runner

# GHCR reads this to link the published package to the repo.
LABEL org.opencontainers.image.source="https://github.com/Shahriar-Ferdoush/codemypaper"

ENTRYPOINT ["codemypaper"]
CMD ["--help"]
