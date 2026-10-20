import { expect, test } from "../lib/proof.ts";

test("open a ticket from the unassigned queue", async ({ page, proof }) => {
  await proof.open("/inbox/unassigned");
  const tickets = page.getByRole("listbox", { name: "Tickets" });
  await expect(tickets.getByRole("option").first()).toBeVisible();
  await proof.chapter("Inbox", "The unassigned queue, then one ticket beside it");
  await proof.shot("unassigned", { caption: "The unassigned queue" });

  await tickets.getByRole("option", { name: /Pricing question/ }).click();
  await expect(page.getByRole("main").getByText("Is the Business plan billed per member").last()).toBeVisible();
  await proof.dwell();
  await proof.shot("ticket", { caption: "A ticket open beside the queue" });
});

test("change the contact on a ticket", async ({ page, proof }) => {
  await proof.open("/inbox/unassigned");
  const tickets = page.getByRole("listbox", { name: "Tickets" });
  const pricingQuestion = tickets.getByRole("option", { name: /Pricing question/ });
  await expect(pricingQuestion).toBeVisible();
  await pricingQuestion.click();

  const main = page.getByRole("main");
  await expect(main.getByText("Is the Business plan billed per member or per workspace").last()).toBeVisible();
  const changeContact = main.getByRole("button", { name: "Change contact" });
  await expect(changeContact).toBeVisible();
  await changeContact.click();

  const search = page.getByRole("combobox", { name: "Search contacts" });
  await expect(search).toBeVisible();
  await search.fill("Dana Whitfield");
  const dana = page.getByRole("option", { name: /Dana Whitfield/ });
  await expect(dana).toBeVisible();
  await proof.chapter("Change contact", "Find another contact and assign them to the ticket");
  await expect(dana).toBeVisible();
  await proof.shot("contact-picker", { caption: "Search results for the new contact" });

  await dana.click();
  await expect(main.getByText("Dana Whitfield", { exact: true }).last()).toBeVisible();
  await proof.dwell();
  await expect(main.getByText("Dana Whitfield", { exact: true }).last()).toBeVisible();
  await proof.shot("contact-changed", { caption: "The ticket now belongs to Dana Whitfield" });
});
