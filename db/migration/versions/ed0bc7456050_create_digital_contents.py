"""create digital contents

Revision ID: ed0bc7456050
Revises: 
Create Date: 2019-05-16 16:18:42.969682

"""
from alembic import op
import sqlalchemy as sa


# revision identifiers, used by Alembic.
revision = 'ed0bc7456050'
down_revision = None
branch_labels = None
depends_on = None


def upgrade():
    op.create_table(
        "digital_contents",
        sa.Column('id', sa.Integer, primary_key=True),
        sa.Column('api_key_id', sa.String(), nullable=False),
        sa.Column('digest', sa.Text(), nullable=False),
        sa.Column('sha256', sa.String(), nullable=False)
    )


def downgrade():
    op.drop_table('digital_contents')
