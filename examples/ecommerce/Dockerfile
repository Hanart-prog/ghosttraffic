# Builds one demo backend variant: baseline, candidate, or fixed.
FROM python:3.12-slim

ARG VARIANT
RUN test -n "$VARIANT"

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY shop/ shop/
COPY ${VARIANT}/main.py main.py

USER nobody
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]
