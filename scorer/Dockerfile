FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt ./
RUN pip install --no-cache-dir --timeout 120 --retries 3 \
        torch==2.3.0 --index-url https://download.pytorch.org/whl/cpu && \
    pip install --no-cache-dir --timeout 120 --retries 3 -r requirements.txt

COPY . .

ENV TRANSFORMERS_CACHE=/app/models
ENV HF_HOME=/app/models
ENV SENTENCE_TRANSFORMERS_HOME=/app/models

RUN python -c "\
from sentence_transformers import SentenceTransformer; \
from transformers import pipeline; \
import os; \
os.environ['TRANSFORMERS_CACHE'] = '/app/models'; \
os.environ['HF_HOME'] = '/app/models'; \
SentenceTransformer('sentence-transformers/all-MiniLM-L6-v2', cache_folder='/app/models'); \
pipeline('text-classification', model='unitary/toxic-bert', cache_dir='/app/models'); \
print('Models downloaded and cached successfully') \
"

EXPOSE 8001

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8001"]
