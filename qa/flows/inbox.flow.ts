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
  const exportTicket = tickets.getByRole("option", { name: /Can't export notebook to PDF/ });
  await expect(exportTicket).toBeVisible();
  await exportTicket.click();

  const main = page.getByRole("main");
  await expect(page.getByRole("heading", { name: "Can't export notebook to PDF" })).toBeVisible();
  const changeContact = main.getByRole("button", { name: "Change contact" });
  await expect(changeContact).toBeVisible();
  await changeContact.click();

  const search = page.getByRole("combobox", { name: "Search contacts" });
  await expect(search).toBeVisible();
  await search.pressSequentially("Dana", { delay: 60 });
  const dana = page.getByRole("option", { name: /Dana Whitfield/ });
  await expect(dana).toBeVisible();
  await proof.chapter("Change contact", "Find another contact and make them the ticket's customer");
  await proof.shot("contact-picker", { caption: "Search results for the new contact" });

  await dana.click();
  await expect(main.getByText("Dana Whitfield", { exact: true }).last()).toBeVisible();
  await expect(main.getByText(/changed the contact from .* to Dana Whitfield/)).toBeVisible();
  await proof.dwell();
  await proof.shot("contact-changed", { caption: "Dana Whitfield is the customer now, and the timeline records the change" });
});

test("a live chat keeps the visitor who started it", async ({ page, proof }) => {
  await proof.open("/inbox/unassigned/1086");
  await expect(page.getByRole("heading", { name: "Pricing question" })).toBeVisible();
  const main = page.getByRole("main");
  await expect(main.getByText("Yusuf Moreau", { exact: true }).last()).toBeVisible();
  await expect(main.getByRole("button", { name: "Change contact" })).toHaveCount(0);
  await proof.dwell();
  await proof.shot("chat-contact", { caption: "A live chat's contact has no Change contact button" });
});
