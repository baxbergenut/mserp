import { expect } from "@playwright/test";

export async function runPhoneE2E({ page, base }) {
  await page.goto(`${base}/investors`);
  await page.getByRole("button", { name: "Add investor", exact: true }).click();
  await page.getByLabel("Full name", { exact: true }).fill("Phone Test Investor");
  const phone = page.locator('input[type="tel"]');
  for (const value of ["123456789", "123456789012", "bad4703344443"]) {
    await phone.fill(value);
    expect(await phone.evaluate((input) => input.checkValidity())).toBe(false);
    await expect(phone).toHaveAttribute("aria-invalid", "true");
    await page.getByRole("button", { name: "Create investor", exact: true }).click();
    await expect(phone).toBeVisible();
  }
  await phone.fill("+1 (012) 345-6789");
  await expect(phone).toHaveValue("0123456789");
  expect(await phone.evaluate((input) => input.checkValidity())).toBe(true);
  await page.getByRole("button", { name: "Create investor", exact: true }).click();
  const row = page.getByRole("row").filter({ hasText: "Phone Test Investor" });
  await expect(row).toContainText("+1 (012) 345-6789");
  let investors = await (await page.request.get(`${base}/api/investors`)).json();
  expect(investors.find((value) => value.fullName === "Phone Test Investor").phone).toBe("0123456789");
  const search = await (await page.request.get(`${base}/api/investors?search=${encodeURIComponent("+1 (012) 345-6789")}`)).json();
  expect(search.map((value) => value.fullName)).toContain("Phone Test Investor");
  await page.getByRole("button", { name: "Edit Phone Test Investor", exact: true }).click();
  await phone.fill("123456789");
  await page.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(phone).toBeVisible();
  expect(await phone.evaluate((input) => input.checkValidity())).toBe(false);
  await phone.fill("");
  await page.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(phone).toHaveCount(0);
  investors = await (await page.request.get(`${base}/api/investors`)).json();
  expect(investors.find((value) => value.fullName === "Phone Test Investor").phone).toBeNull();

  await page.goto(`${base}/drivers`);
  const driverRow = page.getByRole("row").filter({ hasText: "E2e Driver" });
  await driverRow.getByRole("button", { name: "Edit", exact: true }).click();
  await phone.fill("+1 (470) 334-4443");
  await expect(phone).toHaveValue("4703344443");
  await page.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(driverRow).toContainText("+1 (470) 334-4443");
  const drivers = await (await page.request.get(`${base}/api/drivers`)).json();
  const driver = drivers.find((value) => value.fullName === "E2e Driver");
  expect(driver.phone).toBe("4703344443");
  investors = await (await page.request.get(`${base}/api/investors`)).json();
  expect(investors.find((value) => value.driverId === driver.id).phone).toBe(driver.phone);
  await page.goto(`${base}/drivers/detail?id=${driver.id}`);
  await expect(page.getByText("+1 (470) 334-4443", { exact: true })).toHaveCount(2);

  await page.goto(`${base}/dispatchers`);
  await page.getByRole("button", { name: "Add dispatcher", exact: true }).click();
  await page.getByLabel("Full name", { exact: true }).fill("Phone Test Dispatcher");
  await phone.fill("+1 (470) 334-4443");
  await page.getByRole("button", { name: "Create dispatcher", exact: true }).click();
  await expect(page.getByRole("row").filter({ hasText: "Phone Test Dispatcher" })).toContainText("+1 (470) 334-4443");

  const session = await (await page.request.get(`${base}/api/auth/session`)).json();
  for (const path of ["drivers", "dispatchers", "investors"]) {
    const response = await page.request.post(`${base}/api/${path}`, {
      headers: { "X-CSRF-Token": session.csrfToken },
      data: { fullName: "Invalid Phone", phone: "123456789", ...(path === "drivers" ? { payType: "cpm", payRate: 0 } : {}) },
    });
    expect(response.status()).toBe(400);
    expect((await response.json()).error).toMatch(/10 digits/);
  }
  console.log("Phone E2E passed: invalid create/edit validation, pasted US formats, leading zeroes, canonical APIs, linked investors, optional blank contacts, formatted searches and driver/dispatcher/investor displays.");
}
