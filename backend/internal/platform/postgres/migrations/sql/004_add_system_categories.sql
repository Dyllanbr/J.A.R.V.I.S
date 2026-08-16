CREATE TABLE categories (
    id TEXT PRIMARY KEY,
    transaction_type TEXT NOT NULL,
    display_name_pt_br TEXT NOT NULL,
    sort_order INTEGER NOT NULL,
    CONSTRAINT categories_id_format CHECK (
        octet_length(id) BETWEEN 1 AND 64
        AND (id COLLATE "C") ~ '^[a-z][a-z0-9]*([._][a-z0-9]+)*$'
    ),
    CONSTRAINT categories_transaction_type_valid CHECK (
        transaction_type IN ('EXPENSE', 'INCOME')
    ),
    CONSTRAINT categories_display_name_valid CHECK (
        display_name_pt_br = btrim(display_name_pt_br, U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
        AND char_length(display_name_pt_br) BETWEEN 1 AND 80
        AND display_name_pt_br !~ '[[:cntrl:]]'
    ),
    CONSTRAINT categories_sort_order_positive CHECK (sort_order > 0),
    CONSTRAINT categories_transaction_type_id_unique UNIQUE (transaction_type, id),
    CONSTRAINT categories_transaction_type_sort_order_unique UNIQUE (transaction_type, sort_order)
);

INSERT INTO categories (id, transaction_type, display_name_pt_br, sort_order) VALUES
    ('expense.food', 'EXPENSE', 'Alimentação', 10),
    ('expense.transport', 'EXPENSE', 'Transporte', 20),
    ('expense.housing', 'EXPENSE', 'Moradia', 30),
    ('expense.health', 'EXPENSE', 'Saúde', 40),
    ('expense.leisure', 'EXPENSE', 'Lazer', 50),
    ('expense.education', 'EXPENSE', 'Educação', 60),
    ('expense.subscriptions', 'EXPENSE', 'Assinaturas', 70),
    ('expense.shopping', 'EXPENSE', 'Compras', 80),
    ('expense.taxes_fees', 'EXPENSE', 'Impostos e taxas', 90),
    ('expense.other', 'EXPENSE', 'Outros', 100),
    ('income.salary', 'INCOME', 'Salário', 10),
    ('income.freelance', 'INCOME', 'Freelance', 20),
    ('income.refund', 'INCOME', 'Reembolso', 30),
    ('income.sale', 'INCOME', 'Venda', 40),
    ('income.investment_return', 'INCOME', 'Rendimentos', 50),
    ('income.benefits', 'INCOME', 'Benefícios', 60),
    ('income.other', 'INCOME', 'Outros', 70);

ALTER TABLE transactions
    ADD COLUMN category_id TEXT,
    ADD CONSTRAINT transactions_type_category_fkey
        FOREIGN KEY (type, category_id)
        REFERENCES categories(transaction_type, id)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT;

---- create above / drop below ----

LOCK TABLE transactions IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM transactions WHERE category_id IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 004 cannot be rolled back while categorized transactions exist';
    END IF;
END
$$;

ALTER TABLE transactions
    DROP CONSTRAINT transactions_type_category_fkey,
    DROP COLUMN category_id;

DROP TABLE categories;
