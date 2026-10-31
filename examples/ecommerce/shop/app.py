"""Application factory shared by the baseline, candidate, and fixed backends."""

from collections.abc import Callable
from typing import Annotated, Any

from fastapi import FastAPI, Query, Request, Response
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field

from shop.store import Conflict, NotFound


class CartCreate(BaseModel):
    customer_id: int


class ItemAdd(BaseModel):
    product_id: int
    quantity: int = Field(gt=0)


class OrderCreate(BaseModel):
    cart_id: int


def create_app(variant: str, probe: Callable[[], str], store: Any) -> FastAPI:
    """Build a backend. `probe` returns the seed digest or raises if the database is down."""
    app = FastAPI(title=f"ghostshop-{variant}")

    @app.exception_handler(NotFound)
    def not_found(_: Request, exc: NotFound) -> JSONResponse:
        return JSONResponse({"error": "not_found", "detail": str(exc)}, status_code=404)

    @app.exception_handler(Conflict)
    def conflict(_: Request, exc: Conflict) -> JSONResponse:
        return JSONResponse({"error": "conflict", "detail": str(exc)}, status_code=409)

    @app.get("/health")
    def health(response: Response) -> dict[str, str | None]:
        try:
            digest = probe()
        except Exception:
            response.status_code = 503
            return {"status": "unavailable", "variant": variant, "seed_digest": None}
        return {"status": "ok", "variant": variant, "seed_digest": digest}

    @app.get("/api/products")
    def search_products(
        query: str = "", limit: Annotated[int, Query(gt=0, le=100)] = 20
    ) -> dict[str, list[dict]]:
        return {"items": store.search_products(query, limit)}

    @app.get("/api/products/{product_id}")
    def get_product(product_id: int) -> dict[str, dict]:
        return {"product": store.get_product(product_id)}

    @app.post("/api/carts", status_code=201)
    def create_cart(body: CartCreate) -> dict[str, dict]:
        return {"cart": store.create_cart(body.customer_id)}

    @app.post("/api/carts/{cart_id}/items", status_code=201)
    def add_item(cart_id: int, body: ItemAdd) -> dict[str, dict]:
        return {"cart": store.add_item(cart_id, body.product_id, body.quantity)}

    @app.get("/api/carts/{cart_id}")
    def get_cart(cart_id: int) -> dict[str, dict]:
        return {"cart": store.get_cart(cart_id)}

    @app.post("/api/orders", status_code=201)
    def create_order(body: OrderCreate) -> dict[str, dict]:
        return {"order": store.create_order(body.cart_id)}

    return app
