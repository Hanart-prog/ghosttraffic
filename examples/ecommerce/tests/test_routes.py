import pytest
from fastapi.testclient import TestClient

from shop.app import create_app
from shop.store import Conflict, NotFound


class FakeStore:
    def search_products(self, query, limit):
        return [{"id": 1, "name": query, "limit": limit}]

    def get_product(self, product_id):
        raise NotFound(f"product {product_id}")

    def create_cart(self, customer_id):
        return {"id": 7, "customer_id": customer_id, "items": []}

    def add_item(self, cart_id, product_id, quantity):
        return {"id": cart_id, "items": [{"product_id": product_id, "quantity": quantity}]}

    def get_cart(self, cart_id):
        return {"id": cart_id, "items": []}

    def create_order(self, cart_id):
        raise Conflict(f"cart {cart_id} is empty")


@pytest.fixture
def client():
    return TestClient(create_app("baseline", probe=lambda: "x", store=FakeStore()))


def test_search(client):
    response = client.get("/api/products", params={"query": "shoe", "limit": 3})
    assert response.json() == {"items": [{"id": 1, "name": "shoe", "limit": 3}]}


def test_search_limit_is_bounded(client):
    assert client.get("/api/products", params={"limit": 101}).status_code == 422


def test_missing_product_is_404(client):
    response = client.get("/api/products/9")
    assert response.status_code == 404
    assert response.json()["error"] == "not_found"


def test_create_cart_ignores_unknown_fields(client):
    response = client.post("/api/carts", json={"customer_id": 3, "password": "x"})
    assert response.status_code == 201
    assert response.json()["cart"]["customer_id"] == 3


def test_add_item_requires_positive_quantity(client):
    body = {"product_id": 1, "quantity": 0}
    assert client.post("/api/carts/1/items", json=body).status_code == 422


def test_order_conflict_is_409(client):
    assert client.post("/api/orders", json={"cart_id": 1}).status_code == 409
