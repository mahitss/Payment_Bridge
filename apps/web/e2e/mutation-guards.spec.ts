import { test, expect } from "@playwright/test";

test.describe("E. Backend Mutation Guards and Readiness Truthfulness", () => {
  let sessionCookie = "";
  let fixtureCaseId = "";
  let fixtureOpId = "";

  test.beforeAll(async ({ request }) => {
    // Authenticate as operator to obtain session
    const loginRes = await request.post("/api/v1/auth/login", {
      data: {
        email: "operator@claimbridge.internal",
        password: "OperatorPass123!",
      },
    });
    expect(loginRes.status()).toBe(200);

    const setCookieHeaders = loginRes
      .headersArray()
      .filter((h) => h.name.toLowerCase() === "set-cookie");
    if (setCookieHeaders.length > 0) {
      sessionCookie = setCookieHeaders[0].value.split(";")[0];
    } else {
      const rawCookie = loginRes.headers()["set-cookie"];
      if (rawCookie) {
        sessionCookie = rawCookie.split(";")[0];
      }
    }

    // Query payout cases to find the seeded synthetic fixture DEMO-W9821
    const casesRes = await request.get("/api/v1/payout-cases?limit=20", {
      headers: sessionCookie ? { Cookie: sessionCookie } : {},
    });
    expect(casesRes.status()).toBe(200);
    const data = await casesRes.json();
    const cases = data.cases || [];
    const fixture = cases.find(
      (c: any) => c.reference === "DEMO-W9821" || c.is_synthetic_fixture === true
    );

    if (fixture) {
      fixtureCaseId = fixture.id;

      // Fetch case details to find its latest operation
      const detailRes = await request.get(`/api/v1/payout-cases/${fixture.id}`, {
        headers: sessionCookie ? { Cookie: sessionCookie } : {},
      });
      if (detailRes.status() === 200) {
        const detailData = await detailRes.json();
        const ops = detailData.operations || [];
        if (ops.length > 0) {
          fixtureOpId = ops[0].id;
        }
      }
    }
  });

  test("server-side mutation guard rejects POST submit on synthetic fixture with 403", async ({
    request,
  }) => {
    if (!fixtureOpId) {
      test.skip(!fixtureOpId, "No synthetic fixture operation found");
      return;
    }

    const res = await request.post(
      `/api/v1/payout-operations/${fixtureOpId}/submit`,
      {
        headers: sessionCookie ? { Cookie: sessionCookie } : {},
      }
    );

    expect(res.status()).toBe(403);
    const body = await res.json();
    expect(body.code).toBe("FIXTURE_EXTERNAL_OPERATION_FORBIDDEN");
    expect(body.error).toContain("forbidden on synthetic demo fixtures");
  });

  test("server-side mutation guard rejects POST sync on synthetic fixture with 403", async ({
    request,
  }) => {
    if (!fixtureCaseId) {
      test.skip(!fixtureCaseId, "No synthetic fixture case found");
      return;
    }

    const res = await request.post(`/api/v1/payout-cases/${fixtureCaseId}/sync`, {
      headers: sessionCookie ? { Cookie: sessionCookie } : {},
    });

    expect(res.status()).toBe(403);
    const body = await res.json();
    expect(body.code).toBe("FIXTURE_EXTERNAL_OPERATION_FORBIDDEN");
    expect(body.error).toContain("forbidden on synthetic demo fixtures");
  });

  test("server-side mutation guard rejects POST cancel on synthetic fixture with 403", async ({
    request,
  }) => {
    if (!fixtureOpId) {
      test.skip(!fixtureOpId, "No synthetic fixture operation found");
      return;
    }

    const res = await request.post(
      `/api/v1/payout-operations/${fixtureOpId}/cancel`,
      {
        headers: sessionCookie ? { Cookie: sessionCookie } : {},
      }
    );

    expect(res.status()).toBe(403);
    const body = await res.json();
    expect(body.code).toBe("FIXTURE_EXTERNAL_OPERATION_FORBIDDEN");
    expect(body.error).toContain("forbidden on synthetic demo fixtures");
  });

  test("sandbox status endpoint accurately reports authentication state without false claims", async ({
    request,
  }) => {
    const res = await request.get("/api/v1/sandbox/status", {
      headers: sessionCookie ? { Cookie: sessionCookie } : {},
    });

    // Endpoint must return a structured response
    const status = res.status();
    expect([200, 401, 502, 503]).toContain(status);

    const body = await res.json();
    expect(body).toHaveProperty("status");
    expect(body).toHaveProperty("paypal_env", "sandbox");

    // If unconfigured or missing credentials, status must NOT claim authenticated
    if (status === 503 || body.status === "unconfigured") {
      expect(body.token_valid).toBe(false);
      expect(body.status).not.toBe("authenticated");
    }
  });
});
