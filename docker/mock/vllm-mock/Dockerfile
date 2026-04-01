# Copyright 2024 The AIBrix Authors
# Copyright 2025 The Volcano Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# Mock image compatible with the vLLM OpenAI API and Prometheus metrics
# (https://github.com/vllm-project/vllm).

FROM python:3.10-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    WANDB_MODE=disabled \
    MODEL_NAME=deepseek-r1-1-5b \
    SIMULATION=disabled

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    jq \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

COPY ./*.py /app/
COPY ./*.json /app/

EXPOSE 8000

CMD ["python", "app.py"]
