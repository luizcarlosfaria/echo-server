using System;
using System.Collections.Generic;
using System.Dynamic;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Authorization;
using Microsoft.AspNetCore.Mvc;
using Newtonsoft.Json.Linq;

namespace Jornada.Controllers
{
    public class DefaultController : Controller
    {
        public DefaultController()
        {
        }

        private bool Accept(string format) => this.Request.Headers["Accept"].FirstOrDefault()?.Contains(format) ?? false;


        public IActionResult Index()
        {
            if (this.Accept("text/html"))
                return this.IndexHtml();
            else if (this.Accept("application/json") || this.Accept("*/*"))
                return this.IndexJson();
            else
                return this.View();
        }

        public IActionResult IndexHtml()
        {
            return this.View("Index");
        }

        string[] verbsWithForms = new string[] { "POST", "PUT" };
        string[] contentTypeWithoutForms = new string[] { "application/json" };

        public IActionResult IndexJson()
        {
            var responseObject = new
            {
                Headers = this.Request.Headers,
                Path = this.Request.Path,
                Method = this.Request.Method,
                Scheme = this.Request.Scheme,
                QueryString = this.Request.Query,
                Forms = this.verbsWithForms.Contains(this.Request.Method) && !this.contentTypeWithoutForms.Contains(this.Request.ContentType) ? this.Request.Form : null,
                EnvironmentVariables = Environment.GetEnvironmentVariables()
            };
            return this.Json(responseObject);
        }


    }
}
