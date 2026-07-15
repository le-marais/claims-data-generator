Our team often requires anonymized individual insurance claims data for reserving demo or testing purposes.

This data has 3 components. Policy data, used for estimating exposure and simulating claim events. Claims data, used for simulating claim case estimate movements and payments to the customer, and lastly, the simulated transactions for the payments and case estimate movements for every claim.

The objective is to build an app which can simulate data for a class of business, e.g. motor personal insurance, which can be used as dummy input to a reserving process.
The class of business we are simulating must represent typical real world data that is parameterized in a way that it can be adjusted by the user of the app.

Extra modelling ideas:
Consider using pricing loss ratio as a parameter or target for the total simulated data for the simulations per class.
premium can be a multiple of sum insured.

for modelling the delay between report and close dates, we factor in the initial case estimate size. this can be a step function that above a certain threshold of claim estimate size, the delay between report and close date increases.

